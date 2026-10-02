package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/analyzer"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/jobs"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk/splunktest"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/watchdog"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/config"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
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
	return vpn.StatusResult{Healthy: f.healthy}
}

type env struct {
	st    *store.Store
	fake  *splunktest.Fake
	vpn   *fakeVPN
	mon   *watchdog.Monitor
	w     *Worker
	svc   *jobs.Service
	user  store.User
	other store.User
	sp    *splunk.Client
}

const sessionJSON = `{"cookies":{"splunkd_8008":"a","session_id_8008":"b","splunkweb_csrf_token_8008":"c","token_key":"d"}}`

func setup(t *testing.T, a analyzer.Analyzer) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fake := splunktest.New()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	sessPath := filepath.Join(dir, "s.json")
	os.WriteFile(sessPath, []byte(sessionJSON), 0o600)
	login := filepath.Join(dir, "login.sh")
	os.WriteFile(login, []byte("#!/bin/sh\nexit 1\n"), 0o755)
	sp := splunk.New(config.Splunk{
		URL: srv.URL, Templates: map[string]string{"prod": "index=app {transaction_id}"}, SessionPath: sessPath,
		ResultWaitTimeout: 2 * time.Second, PollInterval: 5 * time.Millisecond, MaxLogLines: 100,
		LoginCommand: []string{login}, LoginTimeout: 2 * time.Second,
	})
	if err := sp.Load(); err != nil {
		t.Fatal(err)
	}
	v := &fakeVPN{healthy: true}
	mon := watchdog.New(v, sp, nil, "")
	mon.CheckVPN(ctx)
	w := New(st, sp, a, mon, nil, Config{WaitingExpiry: time.Hour, JobTimeout: 10 * time.Second, LogDir: filepath.Join(dir, "logs")})
	u, _ := st.CreateUser(ctx, "qa", "h", store.RoleQA)
	o, _ := st.CreateUser(ctx, "eng", "h", store.RoleEngineer)
	svc := &jobs.Service{Store: st, Environments: []string{"prod"}, Header: "X-Transaction-ID",
		Limits: store.Limits{Total: 10, PerUser: 3}, DedupWindow: 24 * time.Hour, Wake: w.Wake}
	return &env{st: st, fake: fake, vpn: v, mon: mon, w: w, svc: svc, user: u, other: o, sp: sp}
}

func (e *env) submit(t *testing.T, txn string) store.Job {
	t.Helper()
	j, dup, err := e.svc.Submit(context.Background(), e.user, jobs.SubmitRequest{Environment: "prod", TimeRange: "24h", Input: txn, Force: true})
	if err != nil || dup {
		t.Fatalf("submit: %v dup=%v", err, dup)
	}
	return j
}

func (e *env) step(t *testing.T) {
	t.Helper()
	if _, err := e.w.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (e *env) job(t *testing.T, id int64) store.Job {
	t.Helper()
	j, err := e.st.GetJob(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestHappyPathAndNoLogs(t *testing.T) {
	e := setup(t, analyzer.Fake{})
	e.fake.SetLogs("abc-1", "INFO start user=a@b.com", "ERROR ESB timeout Authorization: Bearer sekret")
	j := e.submit(t, "abc-1")
	e.step(t)
	got := e.job(t, j.ID)
	if got.Status != store.StatusDone || got.StartedAt == nil || got.VPNCheckedAt == nil || got.SearchDoneAt == nil || got.AnalyzedAt == nil || got.FinishedAt == nil {
		t.Fatalf("job = %+v", got)
	}
	inv, err := e.st.GetInvestigation(context.Background(), j.ID)
	if err != nil || inv.Summary == "" || len(inv.RelevantLogs) != 1 || inv.LLMFailed || inv.Model != "fake" {
		t.Fatalf("inv = %+v, %v", inv, err)
	}
	for _, leak := range []string{"a@b.com", "sekret"} {
		if strings.Contains(inv.RawLogSnippet, leak) || strings.Contains(strings.Join(inv.RelevantLogs, ""), leak) {
			t.Errorf("%q not redacted: %+v", leak, inv)
		}
	}
	if !strings.HasSuffix(e.fake.Searches[0], "abc-1 NOT kong") {
		t.Errorf("search = %q", e.fake.Searches[0])
	}

	// Dedup returns the previous result unless forced.
	prev, dup, err := e.svc.Submit(context.Background(), e.user, jobs.SubmitRequest{Environment: "prod", TimeRange: "24h", Input: "abc-1"})
	if err != nil || !dup || prev.ID != j.ID {
		t.Fatalf("dedup = %+v %v %v", prev, dup, err)
	}

	n := e.submit(t, "nothing")
	e.step(t)
	if got := e.job(t, n.ID); got.Status != store.StatusNoLogs {
		t.Fatalf("no logs job = %+v", got)
	}
}

func TestVPNDownWaitsThenExpires(t *testing.T) {
	e := setup(t, analyzer.Fake{})
	e.vpn.Set(false)
	j := e.submit(t, "abc-1")
	e.step(t) // live check at job start sees VPN down → back to waiting
	if got := e.job(t, j.ID); got.Status != store.StatusWaitingVPN {
		t.Fatalf("status = %s", got.Status)
	}
	j2 := e.submit(t, "abc-2")
	e.step(t) // monitor now says down → pending jobs held
	if got := e.job(t, j2.ID); got.Status != store.StatusWaitingVPN {
		t.Fatalf("second status = %s", got.Status)
	}
	e.w.Cfg.WaitingExpiry = time.Nanosecond
	time.Sleep(5 * time.Millisecond)
	e.step(t)
	if got := e.job(t, j.ID); got.Status != store.StatusExpired || got.FailureReason == "" {
		t.Fatalf("expired = %+v", got)
	}
}

func TestVPNRecoveryResumes(t *testing.T) {
	e := setup(t, analyzer.Fake{})
	e.vpn.Set(false)
	e.mon.CheckVPN(context.Background())
	j := e.submit(t, "abc-1")
	e.step(t)
	e.vpn.Set(true)
	e.mon.CheckVPN(context.Background())
	e.step(t)
	if got := e.job(t, j.ID); got.Status != store.StatusNoLogs {
		t.Fatalf("after recovery = %s", got.Status)
	}
}

func TestSplunkExpiredPausesQueue(t *testing.T) {
	e := setup(t, analyzer.Fake{})
	e.fake.SetExpired(true)
	j := e.submit(t, "abc-1")
	j2 := e.submit(t, "abc-2")
	e.step(t)
	got := e.job(t, j.ID)
	if got.Status != store.StatusFailed || !strings.Contains(got.FailureReason, "Splunk") {
		t.Fatalf("job = %+v", got)
	}
	if !e.mon.State().SplunkPaused {
		t.Fatal("queue not paused")
	}
	e.step(t)
	if got := e.job(t, j2.ID); got.Status != store.StatusWaitingSplunk {
		t.Fatalf("second job = %s", got.Status)
	}
}

func TestAnalyzerFailureKeepsRawLogs(t *testing.T) {
	e := setup(t, analyzer.Fake{Err: errors.New("boom")})
	e.fake.SetLogs("abc-1", "ERROR x")
	j := e.submit(t, "abc-1")
	e.step(t)
	got := e.job(t, j.ID)
	inv, _ := e.st.GetInvestigation(context.Background(), j.ID)
	if got.Status != store.StatusDone || !inv.LLMFailed || inv.Model != "" || !strings.Contains(inv.RawLogSnippet, "ERROR x") {
		t.Fatalf("job=%+v inv=%+v", got, inv)
	}
}

type blockingAnalyzer struct{}

func (blockingAnalyzer) Analyze(ctx context.Context, _, _ string) (analyzer.Diagnosis, error) {
	<-ctx.Done()
	return analyzer.Diagnosis{}, ctx.Err()
}

func TestRecheckFailsLongJobWhenVPNDrops(t *testing.T) {
	e := setup(t, blockingAnalyzer{})
	e.fake.SetLogs("abc-1", "ERROR x")
	e.w.Cfg.RecheckAfter = 50 * time.Millisecond
	j := e.submit(t, "abc-1")
	go func() {
		time.Sleep(20 * time.Millisecond)
		e.vpn.Set(false)
	}()
	e.step(t)
	got := e.job(t, j.ID)
	if got.Status != store.StatusFailed || !strings.Contains(got.FailureReason, "VPN") {
		t.Fatalf("job = %+v", got)
	}
}

func TestRecoverAndCancel(t *testing.T) {
	e := setup(t, analyzer.Fake{})
	ctx := context.Background()
	j := e.submit(t, "abc-1")
	e.st.SetStatus(ctx, j.ID, nil, store.Transition{Status: store.StatusSearching})
	if err := e.w.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.job(t, j.ID); got.Status != store.StatusFailed {
		t.Fatalf("recover = %s", got.Status)
	}

	k := e.submit(t, "abc-2")
	stranger, _ := e.st.CreateUser(ctx, "qa2", "h", store.RoleQA)
	if _, err := e.svc.Cancel(ctx, stranger, k.ID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("stranger cancel err = %v", err)
	}
	if got, err := e.svc.Cancel(ctx, e.other, k.ID); err != nil || got.Status != store.StatusCancelled {
		t.Fatalf("engineer cancel = %+v %v", got, err)
	}
	if _, err := e.svc.Cancel(ctx, e.user, j.ID); !errors.Is(err, jobs.ErrNotPending) {
		t.Fatalf("cancel finished err = %v", err)
	}
}

func TestSubmitValidation(t *testing.T) {
	e := setup(t, analyzer.Fake{})
	ctx := context.Background()
	var ve *jobs.ValidationError
	for _, req := range []jobs.SubmitRequest{
		{Environment: "dev", TimeRange: "24h", Input: "a"},
		{Environment: "prod", TimeRange: "7d", Input: "a"},
		{Environment: "prod", TimeRange: "24h", Input: "bad id!"},
	} {
		if _, _, err := e.svc.Submit(ctx, e.user, req); !errors.As(err, &ve) {
			t.Errorf("Submit(%+v) err = %v", req, err)
		}
	}
	for i := 0; i < 3; i++ {
		e.submit(t, "t"+string(rune('a'+i)))
	}
	var le *jobs.LimitError
	if _, _, err := e.svc.Submit(ctx, e.user, jobs.SubmitRequest{Environment: "prod", TimeRange: "24h", Input: "td", Force: true}); !errors.As(err, &le) {
		t.Errorf("limit err = %v", err)
	}
}

type recordingAnalyzer struct{ path *string }

func (r recordingAnalyzer) Analyze(_ context.Context, txn, path string) (analyzer.Diagnosis, error) {
	*r.path = path
	return analyzer.Diagnosis{Summary: "ok " + txn, RelevantLogs: []string{}}, nil
}

func TestAnalyzerReadsFullLogFile(t *testing.T) {
	var path string
	e := setup(t, recordingAnalyzer{&path})
	long := "<html>" + strings.Repeat("é", 3000) + "</html>"
	e.fake.SetLogs("abc-1", `{"level":"info","msg":"start"}`, "ERROR mid user a@b.com "+long, `{"level":"error","status":503}`)
	j := e.submit(t, "abc-1")
	e.step(t)
	if got := e.job(t, j.ID); got.Status != store.StatusDone {
		t.Fatalf("job = %+v", got)
	}
	if path != filepath.Join(e.w.Cfg.LogDir, fmt.Sprintf("job-%d.log", j.ID)) {
		t.Fatalf("analyzer got %q", path)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("log file: %v %v", fi, err)
	}
	b, _ := os.ReadFile(path)
	text := string(b)
	for _, want := range []string{"Transaction ID: abc-1", "Events: 3 (oldest first)", "#1 [", "\"msg\": \"start\"", "#3 [", "\"status\": 503"} {
		if !strings.Contains(text, want) {
			t.Errorf("log file missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "a@b.com") {
		t.Errorf("log file not redacted:\n%s", text)
	}
	for _, l := range strings.Split(text, "\n") {
		if len(l) > maxLineChars || !utf8.ValidString(l) {
			t.Fatalf("line not wrapped on a rune boundary: len %d", len(l))
		}
	}
	if !strings.Contains(strings.ReplaceAll(text, "\n", ""), long) {
		t.Errorf("wrapped event lost content")
	}
	if strings.Index(text, "start") > strings.Index(text, "503") {
		t.Errorf("events not oldest first:\n%s", text)
	}

	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(path, old, old)
	if n, err := PurgeLogFiles(e.w.Cfg.LogDir, time.Now().Add(-24*time.Hour)); err != nil || n != 1 {
		t.Fatalf("PurgeLogFiles = %d, %v", n, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("log file not purged: %v", err)
	}
}

func TestFollowsLinkedBackendIDs(t *testing.T) {
	var path string
	e := setup(t, recordingAnalyzer{&path})
	e.w.Cfg.CorrelationMaxIDs = 5
	apiReq := func(id string) string {
		return `{"tags":["API Request"],"data":{"_id":"` + id + `","status":400,"mobileapptransactionid":"C-1"}}`
	}
	// Two client attempts, each logged by the backend under its own _id; the
	// second attempt's backend id has no logs of its own.
	e.fake.SetLogs("C-1", `{"data":{"transactionid":"C-1","msg":"payload"}}`, apiReq("B-1"), apiReq("B-2"))
	e.fake.SetLogs("B-1", apiReq("B-1"), `{"level":"error","data":{"_id":"B-1","msg":"backend validation failed"}}`)
	j := e.submit(t, "C-1")
	e.step(t)
	if got := e.job(t, j.ID); got.Status != store.StatusDone {
		t.Fatalf("job = %+v", got)
	}
	if len(e.fake.Searches) != 3 || !strings.HasSuffix(e.fake.Searches[1], "B-1 NOT kong") || !strings.HasSuffix(e.fake.Searches[2], "B-2 NOT kong") {
		t.Fatalf("searches = %q", e.fake.Searches)
	}
	inv, err := e.st.GetInvestigation(context.Background(), j.ID)
	if err != nil || len(inv.LinkedIDs) != 1 || inv.LinkedIDs[0] != "B-1" {
		t.Fatalf("linked = %v, %v", inv.LinkedIDs, err)
	}
	b, _ := os.ReadFile(path)
	text := string(b)
	for _, want := range []string{"Linked backend IDs: B-1\n", "payload", "backend validation failed"} {
		if !strings.Contains(text, want) {
			t.Errorf("log file missing %q:\n%s", want, text)
		}
	}

	// Searching the backend id directly does not hop again.
	e.fake.Searches = nil
	e.submit(t, "B-1")
	e.step(t)
	if len(e.fake.Searches) != 1 {
		t.Errorf("searches = %q", e.fake.Searches)
	}
}

func TestMergeResultsDedupesAndSorts(t *testing.T) {
	ev := func(ts, raw string) splunk.Event { return splunk.Event{Time: ts, Raw: raw} }
	a := splunk.Result{Status: splunk.ResultSuccess, EventCount: 2, Events: []splunk.Event{
		ev("2026-10-02T09:46:18.266+07:00", "payload"), ev("2026-10-02T09:46:29.176+07:00", "api request"),
	}}
	b := splunk.Result{Status: splunk.ResultSuccess, EventCount: 2, Truncated: true, Events: []splunk.Event{
		ev("2026-10-02T09:46:20.000+07:00", "backend error"), ev("2026-10-02T09:46:29.176+07:00", "api request"),
	}}
	got := mergeResults(a, b)
	var raws []string
	for _, e := range got.Events {
		raws = append(raws, e.Raw)
	}
	if strings.Join(raws, ",") != "payload,backend error,api request" || !got.Truncated || got.EventCount != 4 {
		t.Fatalf("merged = %+v", got)
	}
	if !strings.HasPrefix(got.Logs, "[2026-10-02T09:46:18.266+07:00] payload\n") {
		t.Errorf("logs = %q", got.Logs)
	}
}

func TestFormatLogFileEventMeta(t *testing.T) {
	job := store.Job{TransactionID: "T1", Environment: "dev", TimeRange: "24h"}
	res := splunk.Result{Events: []splunk.Event{
		{Time: "t1", Raw: "with meta", Host: "pod-1", Source: "payment-service", SourceType: "kube:container"},
		{Time: "t2", Raw: "no meta"},
	}}
	got := formatLogFile(job, res, nil)
	if !strings.Contains(got, "#1 [t1]\nhost=pod-1 source=payment-service sourcetype=kube:container\nwith meta\n") {
		t.Errorf("event with metadata:\n%s", got)
	}
	if !strings.Contains(got, "#2 [t2]\nno meta\n") {
		t.Errorf("event without metadata:\n%s", got)
	}
}
