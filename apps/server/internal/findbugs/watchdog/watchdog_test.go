package watchdog

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/vpn"
)

type fakeVPN struct {
	mu      sync.Mutex
	healthy bool
}

func (f *fakeVPN) Set(h bool) { f.mu.Lock(); f.healthy = h; f.mu.Unlock() }
func (f *fakeVPN) Status() vpn.StatusResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return vpn.StatusResult{Healthy: f.healthy, GPStatus: "x"}
}

type fakeSplunk struct {
	checkErr  error
	reauthErr error
	reauths   int
}

func (f *fakeSplunk) Check(context.Context) error { return f.checkErr }
func (f *fakeSplunk) ReAuth(context.Context) error {
	f.reauths++
	if f.reauthErr == nil {
		f.checkErr = nil
	}
	return f.reauthErr
}

type rec struct {
	mu   sync.Mutex
	keys []string
}

func (r *rec) Notify(_ context.Context, key, _ string) {
	r.mu.Lock()
	r.keys = append(r.keys, key)
	r.mu.Unlock()
}

func TestVPNTransitions(t *testing.T) {
	v, n := &fakeVPN{}, &rec{}
	m := New(v, &fakeSplunk{}, n, "")
	wake := m.Subscribe()
	ctx := context.Background()
	if m.CheckVPN(ctx) {
		t.Fatal("should be down")
	}
	m.CheckVPN(ctx) // no repeat alert
	v.Set(true)
	if !m.CheckVPN(ctx) {
		t.Fatal("should be up")
	}
	select {
	case <-wake:
	default:
		t.Error("worker not woken")
	}
	m.MarkVPNDown(ctx, "boom")
	if m.State().VPNHealthy {
		t.Error("MarkVPNDown ignored")
	}
	want := []string{"vpn-down", "vpn-up", "vpn-down"}
	if len(n.keys) != len(want) {
		t.Fatalf("notifications = %v", n.keys)
	}
}

func TestSplunkAutoReauthOnce(t *testing.T) {
	v := &fakeVPN{healthy: true}
	s := &fakeSplunk{checkErr: splunk.ErrSessionExpired, reauthErr: errors.New("2fa timeout")}
	n := &rec{}
	m := New(v, s, n, "")
	ctx := context.Background()
	m.CheckVPN(ctx)
	if m.CheckSplunk(ctx) || !m.State().SplunkPaused || s.reauths != 1 {
		t.Fatalf("first check: state=%+v reauths=%d", m.State(), s.reauths)
	}
	m.CheckSplunk(ctx)
	if s.reauths != 1 {
		t.Errorf("auto re-auth retried: %d", s.reauths)
	}
	s.reauthErr = nil
	if err := m.Reauth(ctx); err != nil {
		t.Fatal(err)
	}
	if st := m.State(); st.SplunkPaused || !st.SplunkOK {
		t.Errorf("after manual reauth = %+v", st)
	}

	v.Set(false)
	m.CheckVPN(ctx)
	if err := m.Reauth(ctx); err == nil {
		t.Error("reauth without VPN should fail")
	}
}

func TestVPNStatusRecordsState(t *testing.T) {
	v := &fakeVPN{}
	m := New(v, &fakeSplunk{}, &rec{}, "")
	ctx := context.Background()
	m.CheckVPN(ctx)
	v.Set(true)
	if s := m.VPNStatus(ctx); !s.Healthy || s.GPStatus != "x" {
		t.Fatalf("status = %+v", s)
	}
	if !m.State().VPNHealthy {
		t.Error("live status not recorded; banner would stay stale")
	}
}
