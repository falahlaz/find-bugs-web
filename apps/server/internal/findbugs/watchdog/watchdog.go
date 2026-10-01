// Package watchdog tracks whether the shared VPN and Splunk sessions are
// usable, alerts on changes and gates the job worker.
package watchdog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/notify"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/vpn"
)

// VPN is the subset of vpn.Manager used here.
type VPN interface {
	Status() vpn.StatusResult
}

// Splunk is the subset of splunk.Client used here.
type Splunk interface {
	Check(ctx context.Context) error
	ReAuth(ctx context.Context) error
}

// State is the last observed connection state.
type State struct {
	VPNHealthy      bool      `json:"vpnHealthy"`
	VPNDetail       string    `json:"vpnDetail,omitempty"`
	VPNCheckedAt    time.Time `json:"vpnCheckedAt"`
	SplunkOK        bool      `json:"splunkOk"`
	SplunkDetail    string    `json:"splunkDetail,omitempty"`
	SplunkCheckedAt time.Time `json:"splunkCheckedAt"`
	// SplunkPaused is set when the session expired and the automatic re-auth
	// failed; the queue waits until a manual re-auth succeeds.
	SplunkPaused bool `json:"splunkPaused"`
	Reauthing    bool `json:"reauthing"`
}

// Monitor owns State.
type Monitor struct {
	vpn       VPN
	splunk    Splunk
	notify    notify.Notifier
	publicURL string

	mu         sync.RWMutex
	st         State
	vpnKnown   bool
	autoTried  bool // auto re-auth already attempted for the current expiry
	listeners  []chan struct{}
	checkVPNMu sync.Mutex
	autoMu     sync.Mutex // serialises AutoReauth between watchdog and worker
}

// New returns a Monitor. Until the first check the VPN counts as down.
func New(v VPN, s Splunk, n notify.Notifier, publicURL string) *Monitor {
	if n == nil {
		n = notify.Nop{}
	}
	return &Monitor{vpn: v, splunk: s, notify: n, publicURL: publicURL}
}

// State returns a snapshot.
func (m *Monitor) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.st
}

// Subscribe returns a channel that receives a value whenever the VPN or
// Splunk becomes usable again (used to wake the worker).
func (m *Monitor) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.listeners = append(m.listeners, ch)
	m.mu.Unlock()
	return ch
}

func (m *Monitor) wake() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.listeners {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (m *Monitor) link() string {
	if m.publicURL == "" {
		return ""
	}
	return "\n" + m.publicURL
}

func (m *Monitor) reset(key string) {
	if r, ok := m.notify.(notify.Resetter); ok {
		r.Reset(key)
	}
}

// CheckVPN runs a live status check and records the result.
func (m *Monitor) CheckVPN(ctx context.Context) bool {
	m.checkVPNMu.Lock()
	defer m.checkVPNMu.Unlock()
	s := m.vpn.Status()
	detail := s.GPStatus
	if !s.Healthy {
		var bad []string
		for _, r := range s.Reach {
			if !r.OK {
				bad = append(bad, r.Host)
			}
		}
		if len(bad) > 0 {
			detail = strings.TrimSpace(detail + "; tidak terjangkau: " + strings.Join(bad, ", "))
		}
	}
	m.mu.Lock()
	was, known := m.st.VPNHealthy, m.vpnKnown
	m.st.VPNHealthy, m.st.VPNDetail, m.st.VPNCheckedAt = s.Healthy, detail, time.Now()
	m.vpnKnown = true
	m.mu.Unlock()

	switch {
	case s.Healthy && !was:
		slog.Info("vpn healthy")
		m.reset("vpn-down")
		if known {
			m.notify.Notify(ctx, "vpn-up", "✅ VPN tersambung lagi. Antrean Find Bugs berjalan kembali.")
		}
		m.wake()
	case !s.Healthy && (was || !known):
		slog.Warn("vpn down", "detail", detail)
		m.reset("vpn-up")
		m.notify.Notify(ctx, "vpn-down", "⚠️ VPN GlobalProtect putus atau sesi habis. Job baru menunggu sampai ada yang login ulang lewat panel VPN."+m.link())
	}
	return s.Healthy
}

// MarkVPNDown records a VPN failure observed by the worker without waiting
// for the next check.
func (m *Monitor) MarkVPNDown(ctx context.Context, detail string) {
	m.mu.Lock()
	was := m.st.VPNHealthy
	m.st.VPNHealthy, m.st.VPNDetail, m.st.VPNCheckedAt = false, detail, time.Now()
	m.vpnKnown = true
	m.mu.Unlock()
	if was {
		m.reset("vpn-up")
		m.notify.Notify(ctx, "vpn-down", "⚠️ VPN GlobalProtect putus: "+detail+m.link())
	}
}

// CheckSplunk validates the Splunk session (only meaningful with VPN up).
// When it finds the session expired it tries one automatic re-auth.
func (m *Monitor) CheckSplunk(ctx context.Context) bool {
	if !m.State().VPNHealthy {
		return false
	}
	err := m.splunk.Check(ctx)
	if err == nil {
		m.splunkOK(ctx)
		return true
	}
	m.mu.Lock()
	m.st.SplunkOK, m.st.SplunkDetail, m.st.SplunkCheckedAt = false, err.Error(), time.Now()
	m.mu.Unlock()
	if errors.Is(err, splunk.ErrSessionExpired) || errors.Is(err, splunk.ErrNoSession) {
		return m.AutoReauth(ctx)
	}
	slog.Warn("splunk check failed", "err", err)
	return false
}

func (m *Monitor) splunkOK(ctx context.Context) {
	m.mu.Lock()
	wasPaused := m.st.SplunkPaused
	m.st.SplunkOK, m.st.SplunkDetail, m.st.SplunkCheckedAt = true, "", time.Now()
	m.st.SplunkPaused, m.autoTried = false, false
	m.mu.Unlock()
	if wasPaused {
		m.reset("splunk-expired")
		m.notify.Notify(ctx, "", "✅ Sesi Splunk aktif lagi. Antrean Find Bugs berjalan kembali.")
	}
	m.wake()
}

// AutoReauth attempts one automatic re-auth per expiry episode. If it was
// already tried, or fails, the queue is paused until a manual re-auth.
func (m *Monitor) AutoReauth(ctx context.Context) bool {
	start := time.Now()
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	// Another caller may have finished a re-auth while we waited.
	if st := m.State(); st.SplunkOK && !st.SplunkCheckedAt.Before(start) {
		return true
	} else if st.SplunkPaused {
		return false
	}
	m.mu.Lock()
	tried := m.autoTried
	m.autoTried = true
	m.mu.Unlock()
	if tried {
		m.pauseSplunk(ctx, "sesi Splunk kedaluwarsa")
		return false
	}
	m.notify.Notify(ctx, "splunk-reauth", "🔐 Sesi Splunk kedaluwarsa, mencoba login otomatis. Approve push 2FA di HP pemilik akun Splunk (maks 5 menit).")
	if err := m.Reauth(ctx); err != nil {
		m.pauseSplunk(ctx, err.Error())
		return false
	}
	return true
}

// Reauth runs a re-auth (manual from the panel or automatic) and updates state.
func (m *Monitor) Reauth(ctx context.Context) error {
	if !m.State().VPNHealthy {
		return errors.New("VPN belum tersambung; sambungkan VPN dulu")
	}
	m.mu.Lock()
	m.st.Reauthing = true
	m.mu.Unlock()
	err := m.splunk.ReAuth(ctx)
	m.mu.Lock()
	m.st.Reauthing = false
	m.mu.Unlock()
	if errors.Is(err, splunk.ErrReauthBusy) {
		return err
	}
	if err == nil {
		err = m.splunk.Check(ctx)
	}
	if err != nil {
		slog.Warn("splunk re-auth failed", "err", err)
		m.mu.Lock()
		m.st.SplunkOK, m.st.SplunkDetail, m.st.SplunkCheckedAt = false, err.Error(), time.Now()
		m.mu.Unlock()
		return err
	}
	m.splunkOK(ctx)
	return nil
}

func (m *Monitor) pauseSplunk(ctx context.Context, reason string) {
	m.mu.Lock()
	was := m.st.SplunkPaused
	m.st.SplunkPaused, m.st.SplunkOK, m.st.SplunkDetail = true, false, reason
	m.mu.Unlock()
	if !was {
		m.notify.Notify(ctx, "splunk-expired", fmt.Sprintf("⚠️ Sesi Splunk kedaluwarsa dan login otomatis gagal (%s). Antrean dijeda; klik Re-auth di panel Splunk.%s", reason, m.link()))
	}
}

// Run checks VPN and Splunk every interval until ctx is done.
func (m *Monitor) Run(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if m.CheckVPN(ctx) && !m.State().SplunkPaused {
			m.CheckSplunk(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
