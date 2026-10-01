package worker

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	w := New(st, sp, a, mon, nil, Config{WaitingExpiry: time.Hour, JobTimeout: 10 * time.Second})
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
	if err != nil || inv.Summary == "" || len(inv.RelevantLogs) != 1 || inv.LLMFailed {
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
	if got.Status != store.StatusDone || !inv.LLMFailed || !strings.Contains(inv.RawLogSnippet, "ERROR x") {
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
