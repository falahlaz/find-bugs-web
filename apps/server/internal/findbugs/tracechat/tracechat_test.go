package tracechat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/analyzer"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/configrepo"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

type fakeRepos struct{ pruned []string }

func (f *fakeRepos) Sync(_ context.Context, c, ref string) (repos.Checkout, error) {
	if ref == "broken" {
		return repos.Checkout{}, errors.New("fetch failed")
	}
	return repos.Checkout{Container: c, Project: "g/" + c, Dir: "/w/g/" + c + "@" + ref, Ref: ref, Commit: ref}, nil
}

func (f *fakeRepos) Prune(_ context.Context, _ time.Duration, keep func(string) bool) (int, error) {
	n := 0
	for _, d := range []string{"/w/g/svc@1", "/w/g/svc@old"} {
		if !keep(d) {
			f.pruned = append(f.pruned, d)
			n++
		}
	}
	return n, nil
}

type fakeGitLab struct{}

func (fakeGitLab) DeployedCommit(_ context.Context, _, env string, before time.Time) (gitlab.Deployment, error) {
	if env == "preprod" {
		return gitlab.Deployment{}, fmt.Errorf("x: %w", gitlab.ErrNotFound)
	}
	return gitlab.Deployment{Environment: env, Ref: "9.4.1", SHA: "sha-" + env, JobURL: "https://g/jobs/1", FinishedAt: time.Date(2026, 9, 23, 16, 40, 0, 0, time.UTC)}, nil
}

func (fakeGitLab) ResolveRef(_ context.Context, _, ref string) (string, error) {
	if ref == "nope" {
		return "", gitlab.ErrNotFound
	}
	return ref, nil
}

func (fakeGitLab) Branches(_ context.Context, _, search string) ([]gitlab.Branch, error) {
	return []gitlab.Branch{{Name: "hotfix/" + search, Commit: "b1"}}, nil
}

type env struct {
	st    *store.Store
	svc   *Service
	repos *fakeRepos
	user  int64
	job   int64
}

func setup(t *testing.T, tracer analyzer.Tracer) env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	u, _ := st.CreateUser(ctx, "eng", "h", store.RoleEngineer)
	j, err := st.EnqueueJob(ctx, store.NewJob{UserID: u.ID, TransactionID: "T", Environment: "dev", TimeRange: "24h", InputKind: "id"}, store.Limits{Total: 5, PerUser: 5})
	if err != nil {
		t.Fatal(err)
	}
	st.SaveInvestigation(ctx, store.Investigation{JobID: j.ID, Summary: "boom", RelevantLogs: []string{},
		CodeTrace: &store.CodeTrace{Status: store.TraceFound, Project: "g/svc", Commit: "1", File: "a.js", Line: 3, Explanation: "null"}})
	dir := filepath.Join(t.TempDir(), "job")
	os.MkdirAll(dir, 0o700)
	st.SaveTraceSession(ctx, store.TraceSession{JobID: j.ID, SessionID: "sid", Dir: dir,
		Repos: []store.TraceRepo{{Container: "svc", Project: "g/svc", Dir: "/w/g/svc@1", Commit: "1", Ref: "main", RefSource: store.RefFallback}}})
	fr := &fakeRepos{}
	logDir := t.TempDir()
	os.WriteFile(filepath.Join(logDir, fmt.Sprintf("job-%d.log", j.ID)), []byte("ERROR boom\n"), 0o600)
	svc := New(st, tracer, fr, fakeGitLab{}, Config{
		GitLabURL: "https://gitlab", Envs: []string{"dev", "preprod", "production"}, Idle: 10 * time.Minute,
		Retention: 24 * time.Hour, WorktreeTTL: time.Hour, Concurrency: 1, ClaudeDir: t.TempDir(), LogDir: logDir,
	}, ctx)
	return env{st: st, svc: svc, repos: fr, user: u.ID, job: j.ID}
}

// wait returns the session once no turn is running.
func (e env) wait(t *testing.T) View {
	t.Helper()
	for i := 0; i < 200; i++ {
		v, err := e.svc.Get(context.Background(), e.job)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Busy {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("turn did not finish")
	return View{}
}

func TestAskAndRetrace(t *testing.T) {
	e := setup(t, analyzer.Fake{})
	ctx := context.Background()
	if err := e.svc.Ask(ctx, e.job, e.user, "  "); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("empty question err = %v", err)
	}
	if err := e.svc.Ask(ctx, e.job, e.user, "kenapa null?"); err != nil {
		t.Fatal(err)
	}
	v := e.wait(t)
	if len(v.Messages) != 2 || v.Messages[1].Content != "Jawaban palsu untuk: kenapa null?" || v.Messages[1].Status != store.TraceMsgDone ||
		len(v.Messages[1].Progress) != 1 || v.Repos[0].Dir != "" || v.State != store.TraceOpen {
		t.Fatalf("after ask: %+v", v)
	}

	// Re-trace on the commit deployed now to an environment.
	if err := e.svc.Retrace(ctx, e.job, e.user, RetraceRequest{Project: "g/svc", Env: "production"}); err != nil {
		t.Fatal(err)
	}
	v = e.wait(t)
	m := v.Messages[3]
	if v.Messages[2].Content != "Trace ulang g/svc di deploy terbaru di production" || m.Status != store.TraceMsgDone || m.CodeTrace == nil {
		t.Fatalf("retrace messages: %+v", v.Messages)
	}
	ct := m.CodeTrace
	if ct.Commit != "sha-production" || ct.Ref != "9.4.1" || ct.RefSource != store.RefManual || ct.Env != "production" || ct.Line != 2 ||
		ct.URL != "https://gitlab/g/svc/-/blob/sha-production/README.md#L2" || ct.DeployedAt != "2026-09-23T16:40:00Z" {
		t.Fatalf("retrace = %+v", ct)
	}
	ts, _ := e.st.GetTraceSession(ctx, e.job)
	if len(ts.Repos) != 2 || ts.Repos[1].Dir != "/w/g/svc@sha-production" {
		t.Fatalf("repos after retrace = %+v", ts.Repos)
	}

	// A branch that does not exist fails the turn, not the request.
	if err := e.svc.Retrace(ctx, e.job, e.user, RetraceRequest{Project: "g/svc", Ref: "nope"}); err != nil {
		t.Fatal(err)
	}
	if v := e.wait(t); v.Messages[5].Status != store.TraceMsgFailed || !strings.Contains(v.Messages[5].Error, `"nope"`) {
		t.Fatalf("missing ref turn = %+v", v.Messages[5])
	}
	for _, req := range []RetraceRequest{{Project: "g/other", Env: "dev"}, {Project: "g/svc"}, {Project: "g/svc", Env: "dev", Ref: "x"}, {Project: "g/svc", Env: "qa"}} {
		if err := e.svc.Retrace(ctx, e.job, e.user, req); !errors.Is(err, ErrBadRequest) {
			t.Errorf("Retrace(%+v) err = %v", req, err)
		}
	}

	refs, err := e.svc.Refs(ctx, e.job, "g/svc", "fix")
	if err != nil || len(refs.Deployments) != 3 || refs.Deployments[0].Commit != "sha-dev" || refs.Deployments[1].Err != "Belum pernah di-deploy" ||
		len(refs.Branches) != 1 || refs.Branches[0].Name != "hotfix/fix" {
		t.Fatalf("Refs = %+v, %v", refs, err)
	}
}

func TestFailedTurn(t *testing.T) {
	e := setup(t, analyzer.Fake{Err: errors.New("cli down")})
	if err := e.svc.Ask(context.Background(), e.job, e.user, "x"); err != nil {
		t.Fatal(err)
	}
	if v := e.wait(t); v.Messages[1].Status != store.TraceMsgFailed || !strings.Contains(v.Messages[1].Error, "cli down") || v.Busy {
		t.Fatalf("failed turn = %+v", v)
	}
}

func TestCloseReopenAndCleanup(t *testing.T) {
	e := setup(t, analyzer.Fake{})
	ctx := context.Background()
	if err := e.svc.Close(ctx, e.job); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Ask(ctx, e.job, e.user, "x"); !errors.Is(err, ErrClosed) {
		t.Fatalf("ask on closed session err = %v", err)
	}
	e.svc.Reopen(ctx, e.job)
	if v, _ := e.svc.Get(ctx, e.job); v.State != store.TraceOpen {
		t.Fatalf("reopened state = %s", v.State)
	}

	// Idle sessions show as closed without being stored as such.
	e.svc.Cfg.Idle = time.Nanosecond
	time.Sleep(time.Millisecond)
	if v, _ := e.svc.Get(ctx, e.job); v.State != store.TraceClosed || !v.IdleClosed {
		t.Fatalf("idle state = %+v", v)
	}
	if err := e.svc.Ask(ctx, e.job, e.user, "x"); !errors.Is(err, ErrClosed) {
		t.Fatalf("ask on idle session err = %v", err)
	}
	e.svc.Reopen(ctx, e.job)
	e.svc.Cfg.Idle = time.Hour
	if err := e.svc.Ask(ctx, e.job, e.user, "x"); err != nil {
		t.Fatalf("ask after reopen err = %v", err)
	}
	e.wait(t)

	// Cleanup deletes the CLI files of stale sessions and unused worktrees.
	ts, _ := e.st.GetTraceSession(ctx, e.job)
	transcript := filepath.Join(e.svc.Cfg.ClaudeDir, "projects", "-tmp-job", ts.SessionID+".jsonl")
	os.MkdirAll(filepath.Dir(transcript), 0o700)
	os.WriteFile(transcript, []byte("{}"), 0o600)
	if n, w, err := e.svc.Cleanup(ctx); err != nil || n != 0 || w != 1 || e.repos.pruned[0] != "/w/g/svc@old" {
		t.Fatalf("Cleanup of a fresh session = %d, %d, %v, %v", n, w, err, e.repos.pruned)
	}
	e.svc.Cfg.Retention = time.Nanosecond
	time.Sleep(time.Millisecond)
	e.repos.pruned = nil
	if n, w, err := e.svc.Cleanup(ctx); err != nil || n != 1 || w != 2 {
		t.Fatalf("Cleanup = %d, %d, %v", n, w, err)
	}
	if _, err := os.Stat(transcript); !os.IsNotExist(err) {
		t.Fatal("transcript not deleted")
	}
	if _, err := os.Stat(ts.Dir); !os.IsNotExist(err) {
		t.Fatal("session dir not deleted")
	}
	v, _ := e.svc.Get(ctx, e.job)
	if v.State != store.TracePurged || len(v.Messages) != 2 {
		t.Fatalf("purged view = %+v", v)
	}

	// Reopening a purged session starts a new conversation with a recap and
	// the job's logs back in its directory.
	e.svc.Cfg.Retention = 24 * time.Hour
	e.svc.Reopen(ctx, e.job)
	if err := e.svc.Ask(ctx, e.job, e.user, "lagi"); err != nil {
		t.Fatal(err)
	}
	v = e.wait(t)
	if last := v.Messages[len(v.Messages)-1]; last.Content != "Jawaban palsu untuk: lagi (sesi dimulai ulang)" {
		t.Fatalf("restarted answer = %+v", last)
	}
	ts2, _ := e.st.GetTraceSession(ctx, e.job)
	if ts2.SessionID == ts.SessionID || ts2.Restart {
		t.Fatalf("restarted session = %+v", ts2)
	}
	if b, err := os.ReadFile(filepath.Join(ts2.Dir, analyzer.LogFileName)); err != nil || string(b) != "ERROR boom\n" {
		t.Fatalf("logs after restart = %q, %v", b, err)
	}
	if r := e.svc.recap(ctx, e.job); !strings.Contains(r, "First trace (g/svc @ 1): found a.js:3") || !strings.Contains(r, "Engineer: x") {
		t.Fatalf("recap:\n%s", r)
	}
}

func TestBusy(t *testing.T) {
	block := make(chan struct{})
	e := setup(t, blockingTracer{Fake: analyzer.Fake{}, block: block})
	ctx := context.Background()
	if err := e.svc.Ask(ctx, e.job, e.user, "a"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Ask(ctx, e.job, e.user, "b"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second ask err = %v", err)
	}
	close(block)
	e.wait(t)
	if err := e.svc.Recover(ctx); err != nil {
		t.Fatal(err)
	}
}

type blockingTracer struct {
	analyzer.Fake
	block chan struct{}
}

func (b blockingTracer) Ask(ctx context.Context, s analyzer.Session, q, recap string, rs []analyzer.Repo, p analyzer.Progress) (analyzer.Answer, error) {
	<-b.block
	return b.Fake.Ask(ctx, s, q, recap, rs, p)
}

type fakeConfig struct{ before []time.Time }

func (f *fakeConfig) Checkout(_ context.Context, env string, before time.Time) (configrepo.Checkout, error) {
	f.before = append(f.before, before)
	v := store.ConfigVersion{Project: "ops/cfg", Env: env, Branch: env, Path: "json-files", Commit: "cfg-" + env, Source: store.RefDeployed}
	return configrepo.Checkout{Version: v, Repo: store.TraceRepo{Kind: store.RepoConfig, Project: "ops/cfg", Dir: "/w/ops/cfg@" + env,
		Path: "json-files", Commit: v.Commit, Ref: env, RefSource: store.RefDeployed, Env: env}}, nil
}

func TestRetraceConfig(t *testing.T) {
	e := setup(t, analyzer.Fake{})
	cfg := &fakeConfig{}
	e.svc.ConfigRepo = cfg
	ctx := context.Background()
	// The first trace read dev's config.
	ts, _ := e.st.GetTraceSession(ctx, e.job)
	ts.Repos = append(ts.Repos, store.TraceRepo{Kind: store.RepoConfig, Project: "ops/cfg", Dir: "/w/ops/cfg@dev0", Path: "json-files",
		Commit: "cfg-dev0", Ref: "dev", RefSource: store.RefDeployed, Env: "dev"})
	if err := e.st.SaveTraceSession(ctx, ts); err != nil {
		t.Fatal(err)
	}

	// An environment re-trace reads that environment's config deployed now.
	if err := e.svc.Retrace(ctx, e.job, e.user, RetraceRequest{Project: "g/svc", Env: "production"}); err != nil {
		t.Fatal(err)
	}
	v := e.wait(t)
	ct := v.Messages[1].CodeTrace
	if ct == nil || ct.Config == nil || ct.Config.Commit != "cfg-production" || ct.Config.URL != "https://gitlab/ops/cfg/-/tree/cfg-production/json-files" {
		t.Fatalf("env retrace config = %+v", ct)
	}
	if len(cfg.before) != 1 || !cfg.before[0].IsZero() {
		t.Fatalf("config looked up before %v", cfg.before)
	}
	ts, _ = e.st.GetTraceSession(ctx, e.job)
	if len(ts.Repos) != 4 || ts.Repos[3].Kind != store.RepoConfig || ts.Repos[3].Env != "production" {
		t.Fatalf("repos after env retrace = %+v", ts.Repos)
	}

	// A branch re-trace keeps the latest config of the session.
	if err := e.svc.Retrace(ctx, e.job, e.user, RetraceRequest{Project: "g/svc", Ref: "main"}); err != nil {
		t.Fatal(err)
	}
	v = e.wait(t)
	if ct := v.Messages[3].CodeTrace; ct == nil || ct.Config == nil || ct.Config.Commit != "cfg-production" || len(cfg.before) != 1 {
		t.Fatalf("branch retrace config = %+v", ct)
	}

	// The config repo is not a re-trace target.
	if err := e.svc.Retrace(ctx, e.job, e.user, RetraceRequest{Project: "ops/cfg", Env: "dev"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("retrace into config err = %v", err)
	}
	if _, err := e.svc.Refs(ctx, e.job, "ops/cfg", ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("refs of config err = %v", err)
	}
}
