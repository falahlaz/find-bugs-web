package splunk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk/splunktest"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/config"
)

func writeSession(t *testing.T, path string) {
	t.Helper()
	body := `{"base_url":"x","cookies":{"splunkd_8008":"a","session_id_8008":"b","splunkweb_csrf_token_8008":"c","token_key":"d"},"csrf_token":"c"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newClient(t *testing.T, url string) (*Client, config.Splunk) {
	t.Helper()
	cfg := config.Splunk{
		URL:               url,
		Templates:         map[string]string{"prod": `index=app "{transaction_id}" | where x={{1}}`},
		SessionPath:       filepath.Join(t.TempDir(), "s.json"),
		SSODomain:         "login.example.com",
		ResultWaitTimeout: 5 * time.Second,
		PollInterval:      10 * time.Millisecond,
		MaxLogLines:       100,
		LoginTimeout:      5 * time.Second,
	}
	return New(cfg), cfg
}

func TestBuildQuery(t *testing.T) {
	c, _ := newClient(t, "http://x")
	q, err := c.BuildQuery("prod", "abc-1")
	if err != nil || q != `index=app "abc-1" | where x={1}` {
		t.Fatalf("BuildQuery = %q, %v", q, err)
	}
	if _, err := c.BuildQuery("nope", "a"); !errors.Is(err, ErrUnknownEnv) {
		t.Errorf("unknown env err = %v", err)
	}
}

func TestSearch(t *testing.T) {
	fake := splunktest.New()
	fake.SetLogs("abc-1", "ERROR boom bearer xyz", "INFO fine")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c, cfg := newClient(t, srv.URL)
	ctx := context.Background()

	if _, err := c.Search(ctx, "prod", "abc-1", "24h"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("no session err = %v", err)
	}
	writeSession(t, cfg.SessionPath)
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	res, err := c.Search(ctx, "prod", "abc-1", "24h")
	if err != nil || res.Status != ResultSuccess || !strings.Contains(res.Logs, "[2026-10-01T08:00:00] ERROR boom") {
		t.Fatalf("Search = %+v, %v", res, err)
	}
	if fake.FormKey != "c" || !strings.Contains(fake.Cookie, "splunkd_8008=a") {
		t.Errorf("headers cookie=%q formkey=%q", fake.Cookie, fake.FormKey)
	}
	if len(fake.Cancelled) != 1 {
		t.Errorf("job not cleaned up: %v", fake.Cancelled)
	}
	if res, err := c.Search(ctx, "prod", "zzz", "48h"); err != nil || res.Status != ResultNoLogs {
		t.Fatalf("no logs = %+v, %v", res, err)
	}
	if err := c.Check(ctx); err != nil {
		t.Fatal(err)
	}
	fake.SetExpired(true)
	if _, err := c.Search(ctx, "prod", "abc-1", "24h"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired err = %v", err)
	}
	if err := c.Check(ctx); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("check expired err = %v", err)
	}
}

func TestSSORedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://login.example.com/saml", http.StatusFound)
	}))
	defer srv.Close()
	c, cfg := newClient(t, srv.URL)
	writeSession(t, cfg.SessionPath)
	c.Load()
	if err := c.Check(context.Background()); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("redirect err = %v", err)
	}
}

func TestReAuth(t *testing.T) {
	c, cfg := newClient(t, "http://x")
	script := filepath.Join(t.TempDir(), "login.sh")
	os.WriteFile(script, []byte(`#!/bin/sh
echo "Filled email: someone@example.com"
echo "CSRF token: secret123"
cat > "$SPLUNK_API_SESSION_PATH" <<'J'
{"cookies":{"splunkd_8008":"a","session_id_8008":"b","splunkweb_csrf_token_8008":"c","token_key":"d"}}
J
`), 0o755)
	c.cfg.LoginCommand = []string{script}
	if err := c.ReAuth(context.Background()); err != nil {
		t.Fatal(err)
	}
	info := c.Info()
	if !info.Loaded || info.LastReauthOK == nil || !*info.LastReauthOK || info.ReauthRunning {
		t.Fatalf("info = %+v", info)
	}
	joined := strings.Join(info.LastReauthLog, "\n")
	if strings.Contains(joined, "someone@example.com") || strings.Contains(joined, "secret123") {
		t.Errorf("reauth log leaks secrets: %s", joined)
	}
	_ = cfg

	c.cfg.LoginCommand = []string{"false"}
	if err := c.ReAuth(context.Background()); err == nil {
		t.Error("failing script should error")
	}
	if info := c.Info(); info.LastReauthOK == nil || *info.LastReauthOK {
		t.Errorf("info after failure = %+v", info)
	}
}

func TestSearchPagesOldestFirst(t *testing.T) {
	fake := splunktest.New()
	lines := make([]string, 2500)
	for i := range lines {
		lines[i] = fmt.Sprintf("event %04d", i)
	}
	fake.SetLogs("abc-1", lines...)
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c, cfg := newClient(t, srv.URL)
	writeSession(t, cfg.SessionPath)
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	c.cfg.MaxLogLines = 5000

	res, err := c.Search(context.Background(), "prod", "abc-1", "24h")
	if err != nil || res.EventCount != 2500 || res.Truncated || len(res.Events) != 2500 || fake.EventPages != 3 {
		t.Fatalf("Search = count %d truncated %v events %d pages %d, %v", res.EventCount, res.Truncated, len(res.Events), fake.EventPages, err)
	}
	if res.Events[0].Raw != "event 0000" || res.Events[2499].Raw != "event 2499" {
		t.Errorf("not oldest first: %q … %q", res.Events[0].Raw, res.Events[2499].Raw)
	}
	if e := res.Events[0]; e.Host != "fake-host" || e.Source != "fake-service" || e.SourceType != "_json" {
		t.Errorf("metadata = host %q source %q sourcetype %q", e.Host, e.Source, e.SourceType)
	}

	// Over the cap: only the newest MaxLogLines, still oldest first.
	c.cfg.MaxLogLines = 1200
	res, err = c.Search(context.Background(), "prod", "abc-1", "24h")
	if err != nil || !res.Truncated || res.EventCount != 2500 || len(res.Events) != 1200 {
		t.Fatalf("capped Search = count %d truncated %v events %d, %v", res.EventCount, res.Truncated, len(res.Events), err)
	}
	if res.Events[0].Raw != "event 1300" || res.Events[1199].Raw != "event 2499" {
		t.Errorf("capped range: %q … %q", res.Events[0].Raw, res.Events[1199].Raw)
	}
}
