package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/analyzer"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/jobs"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk/splunktest"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/watchdog"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/worker"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/auth"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/config"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/vpn"
)

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	st     *store.Store
	wk     *worker.Worker
	mon    *watchdog.Monitor
	splunk *splunktest.Fake
	api    *API
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	fake := splunktest.New()
	ss := httptest.NewServer(fake)
	t.Cleanup(ss.Close)
	sess := filepath.Join(dir, "s.json")
	os.WriteFile(sess, []byte(`{"cookies":{"splunkd_8008":"a","session_id_8008":"b","splunkweb_csrf_token_8008":"c","token_key":"d"}}`), 0o600)
	scfg := config.Splunk{URL: ss.URL, Templates: map[string]string{"prod": "index=a {transaction_id}", "dev": "index=b {transaction_id}"},
		SessionPath: sess, ResultWaitTimeout: time.Second, PollInterval: 5 * time.Millisecond, MaxLogLines: 100, LoginCommand: []string{"true"}, LoginTimeout: time.Second}
	sp := splunk.New(scfg)
	sp.Load()

	bin, _ := filepath.Abs("../vpn/testdata/fake-globalprotect.sh")
	capture := filepath.Join(dir, "capture.sh")
	os.WriteFile(capture, []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" > '"+filepath.Join(dir, "login-url")+"'\n"), 0o755)
	gp := vpn.NewManager(vpn.Config{Bin: bin, Portal: "p", Dir: dir, Browser: capture})
	t.Cleanup(func() { gp.Disconnect() })
	t.Setenv("FAKE_STATUS", "Connected")

	mon := watchdog.New(gp, sp, nil, "")
	mon.CheckVPN(ctx)
	wk := worker.New(st, sp, analyzer.Fake{}, mon, nil, worker.Config{JobTimeout: 5 * time.Second, LogDir: filepath.Join(dir, "logs")})
	cfg := config.Config{QueueMax: 10, QueuePerUser: 3, Location: time.UTC}
	a := &API{
		Cfg: cfg, Store: st, Auth: auth.NewService(st, time.Hour, false),
		Jobs: &jobs.Service{Store: st, Environments: []string{"prod", "dev"}, Header: "X-Transaction-ID",
			Limits: store.Limits{Total: 10, PerUser: 3}, DedupWindow: 24 * time.Hour, Wake: wk.Wake},
		VPN: gp, Splunk: sp, Monitor: mon, Version: "test", LogDir: filepath.Join(dir, "logs"),
		Web: fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}, "assets/app.js": {Data: []byte("js")}},
	}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	for _, u := range []struct{ name, role string }{{"qa1", store.RoleQA}, {"qa2", store.RoleQA}, {"eng", store.RoleEngineer}} {
		h, _ := auth.HashPassword("password123")
		st.CreateUser(ctx, u.name, h, u.role)
	}
	return &harness{t: t, srv: srv, st: st, wk: wk, mon: mon, splunk: fake, api: a}
}

type client struct {
	h    *harness
	hc   *http.Client
	csrf string
}

func (h *harness) login(user string) *client {
	jar, _ := cookiejar.New(nil)
	c := &client{h: h, hc: &http.Client{Jar: jar}}
	var resp SessionResponse
	if code := c.do("POST", "/api/auth/login", LoginRequest{Username: user, Password: "password123"}, &resp); code != 200 {
		h.t.Fatalf("login %s: %d", user, code)
	}
	c.csrf = resp.CSRFToken
	return c
}

func (c *client) do(method, path string, body, out any) int {
	c.h.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, c.h.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set(auth.CSRFHeader, c.csrf)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestJobFlowAndRoles(t *testing.T) {
	h := newHarness(t)
	qa, qa2, eng := h.login("qa1"), h.login("qa2"), h.login("eng")
	h.splunk.SetLogs("abc-1", "ERROR boom user a@b.com")

	var envs EnvironmentsResponse
	if qa.do("GET", "/api/environments", nil, &envs); strings.Join(envs.Environments, ",") != "dev,prod" {
		t.Errorf("envs = %+v", envs)
	}
	var sub SubmitJobResponse
	code := qa.do("POST", "/api/jobs", SubmitJobRequest{Environment: "prod", TimeRange: "24h", Input: `curl -H "X-Transaction-ID: abc-1" https://x`}, &sub)
	if code != 202 || sub.Duplicate || sub.Job.Status != store.StatusQueued || sub.Job.QueuePosition == nil {
		t.Fatalf("submit = %d %+v", code, sub)
	}
	if code := qa.do("POST", "/api/jobs", SubmitJobRequest{Environment: "prod", TimeRange: "24h", Input: "bad id!"}, nil); code != 400 {
		t.Errorf("invalid input: %d", code)
	}
	if _, err := h.wk.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := "/api/jobs/" + itoa(sub.Job.ID)
	var qaView, engView JobView
	qa.do("GET", path, nil, &qaView)
	eng.do("GET", path, nil, &engView)
	if qaView.Status != store.StatusDone || qaView.Result == nil || qaView.Result.Summary == "" || qaView.Result.QAMessage == "" {
		t.Fatalf("qa view = %+v", qaView)
	}
	if qaView.Result.RawLogSnippet != "" || qaView.Result.LikelyCause != "" || len(qaView.Result.RelevantLogs) != 0 {
		t.Errorf("qa sees engineer fields: %+v", qaView.Result)
	}
	if engView.Result == nil || engView.Result.RawLogSnippet == "" || engView.Result.ErrorType == "" {
		t.Errorf("engineer view = %+v", engView.Result)
	}
	if code := qa2.do("GET", path, nil, nil); code != 404 {
		t.Errorf("other QA can see job: %d", code)
	}

	// Only engineers download the log file; it is gone once purged.
	if code := qa.do("GET", path+"/logs", nil, nil); code != 403 {
		t.Errorf("qa download: %d", code)
	}
	resp, err := eng.hc.Get(h.srv.URL + path + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Disposition") != `attachment; filename="job-`+itoa(sub.Job.ID)+`-abc-1.log"` || !strings.Contains(string(body), "ERROR boom user a@b.com") {
		t.Errorf("download = %d %v %q", resp.StatusCode, resp.Header, body)
	}
	if _, err := worker.PurgeLogFiles(h.api.LogDir, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var gone struct{ Code string }
	if code := eng.do("GET", path+"/logs", nil, &gone); code != 404 || gone.Code != "logs_expired" {
		t.Errorf("purged download = %d %+v", code, gone)
	}
	var list JobListResponse
	qa2.do("GET", "/api/jobs", nil, &list)
	if len(list.Jobs) != 0 {
		t.Errorf("qa2 list = %+v", list)
	}
	eng.do("GET", "/api/jobs?environment=prod&from=2000-01-01", nil, &list)
	if len(list.Jobs) != 1 {
		t.Errorf("engineer list = %+v", list)
	}

	// Duplicate within 24h returns the previous result.
	var dup SubmitJobResponse
	if code := qa2.do("POST", "/api/jobs", SubmitJobRequest{Environment: "prod", TimeRange: "24h", Input: "abc-1"}, &dup); code != 200 || !dup.Duplicate || dup.Job.ID != sub.Job.ID {
		t.Errorf("dup = %d %+v", code, dup)
	}

	// Cancel a queued job.
	var q SubmitJobResponse
	qa.do("POST", "/api/jobs", SubmitJobRequest{Environment: "dev", TimeRange: "48h", Input: "zzz"}, &q)
	if code := qa2.do("POST", "/api/jobs/"+itoa(q.Job.ID)+"/cancel", nil, nil); code != 404 {
		t.Errorf("qa2 cancel: %d", code)
	}
	var cancelled JobView
	if code := qa.do("POST", "/api/jobs/"+itoa(q.Job.ID)+"/cancel", nil, &cancelled); code != 200 || cancelled.Status != store.StatusCancelled {
		t.Errorf("cancel = %d %+v", code, cancelled)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestAuthAndCSRF(t *testing.T) {
	h := newHarness(t)
	anon := &client{h: h, hc: &http.Client{}}
	if code := anon.do("GET", "/api/me", nil, nil); code != 401 {
		t.Errorf("anon me: %d", code)
	}
	if code := anon.do("POST", "/api/auth/login", LoginRequest{Username: "qa1", Password: "nope-nope"}, nil); code != 401 {
		t.Errorf("bad login: %d", code)
	}
	qa := h.login("qa1")
	csrf := qa.csrf
	qa.csrf = ""
	if code := qa.do("POST", "/api/jobs", SubmitJobRequest{Environment: "prod", TimeRange: "24h", Input: "x"}, nil); code != 403 {
		t.Errorf("missing csrf: %d", code)
	}
	qa.csrf = csrf
	if code := qa.do("GET", "/api/users", nil, nil); code != 403 {
		t.Errorf("qa users: %d", code)
	}
	eng := h.login("eng")
	var created store.User
	if code := eng.do("POST", "/api/users", CreateUserRequest{Username: "new.user", Password: "password123", Role: "qa"}, &created); code != 201 {
		t.Fatalf("create user: %d", code)
	}
	inactive := false
	if code := eng.do("PATCH", "/api/users/"+itoa(created.ID), UpdateUserRequest{Active: &inactive}, nil); code != 200 {
		t.Errorf("deactivate: %d", code)
	}
	var me SessionResponse
	eng.do("GET", "/api/me", nil, &me)
	if code := eng.do("PATCH", "/api/users/"+itoa(me.User.ID), UpdateUserRequest{Active: &inactive}, nil); code != 400 {
		t.Errorf("self lockout: %d", code)
	}
	var audit AuditListResponse
	eng.do("GET", "/api/audit", nil, &audit)
	if len(audit.Entries) != 2 {
		t.Errorf("audit = %+v", audit)
	}
	if code := qa.do("POST", "/api/auth/logout", nil, nil); code != 200 {
		t.Errorf("logout: %d", code)
	}
	if code := qa.do("GET", "/api/me", nil, nil); code != 401 {
		t.Errorf("after logout: %d", code)
	}
}

func TestVPNFlowAndStatus(t *testing.T) {
	h := newHarness(t)
	qa := h.login("qa1")
	var st VPNStateResponse
	if code := qa.do("POST", "/api/vpn/connect", nil, &st); code != 202 || st.State != vpn.StateConnecting {
		t.Fatalf("connect = %d %+v", code, st)
	}
	var sys SystemStatus
	qa.do("GET", "/api/system/status", nil, &sys)
	if sys.VPN.Operator != "qa1" || len(sys.Banners) == 0 {
		t.Errorf("status during connect = %+v", sys)
	}
	eng := h.login("eng")
	if code := eng.do("POST", "/api/vpn/connect", nil, nil); code != 409 {
		t.Errorf("concurrent connect: %d", code)
	}
	var lu LoginURLResponse
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		qa.do("GET", "/api/vpn/login-url", nil, &lu)
		if lu.URL != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lu.URL == nil || !strings.HasPrefix(*lu.URL, "https://login.example.com") {
		t.Fatalf("login url = %+v", lu)
	}
	if code := eng.do("POST", "/api/vpn/callback", CallbackRequest{URI: "https://evil"}, nil); code != 400 {
		t.Errorf("bad callback: %d", code)
	}
	var cb vpn.CallbackResult
	if code := eng.do("POST", "/api/vpn/callback", CallbackRequest{URI: "globalprotectcallback:ok&token=s3cret"}, &cb); code != 200 || cb.State != vpn.StateConnected {
		t.Fatalf("callback = %d %+v", code, cb)
	}
	if strings.Contains(cb.Stdout+cb.Stderr, "s3cret") {
		t.Error("callback output leaks token")
	}
	var full vpn.StatusResult
	qa.do("GET", "/api/vpn/status", nil, &full)
	if !full.Healthy || full.ConnectedBy != "eng" {
		t.Errorf("vpn status = %+v", full)
	}
	var health HealthResponse
	anon := &client{h: h, hc: &http.Client{}}
	if code := anon.do("GET", "/healthz", nil, &health); code != 200 || !health.DB {
		t.Errorf("health = %d %+v", code, health)
	}
}

func TestSPAAndOpenAPI(t *testing.T) {
	h := newHarness(t)
	for path, want := range map[string]string{"/": "app", "/jobs/12": "app", "/assets/app.js": "js"} {
		resp, err := http.Get(h.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		resp.Body.Close()
		if !strings.Contains(buf.String(), want) {
			t.Errorf("%s = %q", path, buf.String())
		}
	}
	resp, _ := http.Get(h.srv.URL + "/api/nope")
	if resp.StatusCode != 404 {
		t.Errorf("unknown api: %d", resp.StatusCode)
	}
	var spec map[string]any
	resp, _ = http.Get(h.srv.URL + "/api/openapi.json")
	json.NewDecoder(resp.Body).Decode(&spec)
	paths, _ := spec["paths"].(map[string]any)
	if _, ok := paths["/api/jobs/{id}"]; !ok || spec["openapi"] != "3.0.3" {
		t.Errorf("spec paths = %v", paths)
	}
}
