package vpn

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	bin, err := filepath.Abs("testdata/fake-globalprotect.sh")
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "capture-url.sh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" > '" + filepath.Join(dir, "login-url") + "'\n"
	if err := os.WriteFile(capture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager(Config{Bin: bin, Portal: "portal.example", Dir: dir, Browser: capture, ReachHosts: nil})
	t.Cleanup(func() { m.stopConnect(true) })
	return m
}

func waitLoginURL(t *testing.T, m *Manager) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, url := m.LoginURL(); url != "" {
			if st != StateWaitingCallback {
				t.Fatalf("state = %s, want WAITING_CALLBACK", st)
			}
			return url
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("login URL not captured")
	return ""
}

func assertNoSecrets(t *testing.T, m *Manager, extra ...string) {
	t.Helper()
	b, _ := os.ReadFile(m.cfg.connectLogPath())
	all := append([]string{string(b), strings.Join(m.Logs(), "\n")}, extra...)
	for _, s := range all {
		for _, secret := range []string{"supersecret", "secretcookie", "tok3n"} {
			if strings.Contains(s, secret) {
				t.Errorf("secret %q leaked in:\n%s", secret, s)
			}
		}
	}
}

func TestValidateCallback(t *testing.T) {
	ok := "globalprotectcallback:abc"
	bad := []string{
		"https://x",
		ok + "\nrm -rf",
		"globalprotectcallback:" + strings.Repeat("a", maxURILen),
	}
	for _, uri := range bad {
		if err := validateCallback(uri); !errors.Is(err, ErrInvalid) {
			t.Errorf("validateCallback(%.30q) = %v, want ErrInvalid", uri, err)
		}
	}
	if err := validateCallback(ok); err != nil {
		t.Errorf("valid uri rejected: %v", err)
	}
}

func TestFlow(t *testing.T) {
	m := newTestManager(t)
	if st, err := m.Connect("alice"); err != nil || st != StateConnecting {
		t.Fatalf("Connect = %s, %v", st, err)
	}
	if _, err := m.Connect("alice"); !errors.Is(err, ErrBadState) {
		t.Fatalf("second Connect err = %v, want ErrBadState", err)
	}
	if url := waitLoginURL(t, m); !strings.HasPrefix(url, "https://login.example.com/") {
		t.Fatalf("url = %q", url)
	}
	if s := m.Status(); s.Operator != "alice" {
		t.Errorf("operator = %q, want alice", s.Operator)
	}
	res, err := m.Callback("globalprotectcallback:cas-as=1&token=tok3n", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.State != StateConnected {
		t.Fatalf("callback = %+v", res)
	}
	if !strings.Contains(res.Stdout, "<callback-uri>") {
		t.Errorf("stdout should mask uri: %q", res.Stdout)
	}
	if !m.connectAlive() {
		t.Error("callback should leave connect running")
	}
	t.Setenv("FAKE_STATUS", "Connected")
	if s := m.Status(); !s.Healthy || s.ConnectedBy != "bob" || s.ConnectedAt == nil {
		t.Errorf("status after connect = %+v", s)
	}
	assertNoSecrets(t, m, res.Stdout, res.Stderr)

	if _, err := m.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if m.connectAlive() || m.State() != StateIdle {
		t.Fatalf("after disconnect alive=%v state=%s", m.connectAlive(), m.State())
	}
}

func TestExpiredCallback(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Connect("alice"); err != nil {
		t.Fatal(err)
	}
	waitLoginURL(t, m)
	res, err := m.Callback("globalprotectcallback:expired&token=tok3n", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 1 || res.State != StateFailed {
		t.Fatalf("expired callback = %+v", res)
	}
	if !strings.Contains(res.Stderr, "token=<redacted>") {
		t.Errorf("stderr not redacted: %q", res.Stderr)
	}
	assertNoSecrets(t, m, res.Stdout, res.Stderr)

	// FAILED allows a fresh connect; the old connect process is stopped first.
	if st, err := m.Connect("alice"); err != nil || st != StateConnecting {
		t.Fatalf("reconnect = %s, %v", st, err)
	}
}

func TestBusy(t *testing.T) {
	m := newTestManager(t)
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if _, err := m.Connect("alice"); !errors.Is(err, ErrBusy) {
		t.Errorf("Connect err = %v", err)
	}
	if _, err := m.Callback("globalprotectcallback:x", "alice"); !errors.Is(err, ErrBusy) {
		t.Errorf("Callback err = %v", err)
	}
	if _, err := m.Disconnect(); !errors.Is(err, ErrBusy) {
		t.Errorf("Disconnect err = %v", err)
	}
	// status and logs must not block on the op mutex.
	done := make(chan struct{})
	go func() { m.Status(); m.Logs(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("status/logs blocked")
	}
}

func TestStatus(t *testing.T) {
	m := newTestManager(t)
	ln := httptest.NewServer(http.NotFoundHandler())
	defer ln.Close()
	m.cfg.ReachHosts = []string{strings.TrimPrefix(ln.URL, "http://"), "127.0.0.1:1"}
	t.Setenv("FAKE_STATUS", "Connected")
	s := m.Status()
	if s.State != StateIdle || !strings.Contains(s.GPStatus, "Connected") || s.ConnectAlive {
		t.Fatalf("status = %+v", s)
	}
	if !s.Reach[0].OK || s.Reach[1].OK {
		t.Fatalf("reach = %+v", s.Reach)
	}
	if s.Healthy {
		t.Error("unreachable host must make status unhealthy")
	}
	m.cfg.ReachHosts = m.cfg.ReachHosts[:1]
	if !m.Status().Healthy {
		t.Error("connected + reachable should be healthy")
	}
	t.Setenv("FAKE_STATUS", "Disconnected")
	if m.Status().Healthy {
		t.Error("Disconnected must be unhealthy")
	}
}
