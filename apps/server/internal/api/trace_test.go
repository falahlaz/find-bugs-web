package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/analyzer"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/tracechat"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

type traceRepos struct{}

func (traceRepos) Sync(_ context.Context, c, ref string) (repos.Checkout, error) {
	return repos.Checkout{Container: c, Project: "g/" + c, Dir: "/w/" + ref, Ref: ref, Commit: ref}, nil
}
func (traceRepos) Prune(context.Context, time.Duration, func(string) bool) (int, error) {
	return 0, nil
}

type traceGitLab struct{}

func (traceGitLab) DeployedCommit(_ context.Context, _, env string, _ time.Time) (gitlab.Deployment, error) {
	return gitlab.Deployment{Environment: env, Ref: "9.4.1", SHA: "sha-" + env, FinishedAt: time.Now()}, nil
}
func (traceGitLab) ResolveRef(_ context.Context, _, ref string) (string, error) { return ref, nil }
func (traceGitLab) Branches(context.Context, string, string) ([]gitlab.Branch, error) {
	return []gitlab.Branch{{Name: "main", Commit: "m"}}, nil
}

func TestTraceChat(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	eng, qa := h.login("eng"), h.login("qa1")
	var job SubmitJobResponse
	if code := qa.do("POST", "/api/jobs", SubmitJobRequest{Environment: "prod", TimeRange: "24h", Input: "abc-1"}, &job); code != 202 {
		t.Fatalf("submit = %d", code)
	}
	base := fmt.Sprintf("/api/jobs/%d/trace", job.Job.ID)
	if code := eng.do("GET", base, nil, nil); code != 404 {
		t.Fatalf("trace while disabled = %d", code)
	}
	h.api.Trace = tracechat.New(h.st, analyzer.Fake{}, traceRepos{}, traceGitLab{}, tracechat.Config{
		Envs: []string{"dev", "production"}, Idle: 10 * time.Minute, Concurrency: 1,
	}, ctx)
	if code := eng.do("GET", base, nil, nil); code != 404 {
		t.Fatalf("trace without session = %d", code)
	}
	h.st.SaveTraceSession(ctx, store.TraceSession{JobID: job.Job.ID, SessionID: "s", Dir: t.TempDir(),
		Repos: []store.TraceRepo{{Container: "svc", Project: "g/svc", Dir: "/secret/path", Commit: "1", Ref: "main", RefSource: store.RefFallback}}})

	if code := qa.do("GET", base, nil, nil); code != 403 {
		t.Fatalf("QA trace = %d", code)
	}
	var v TraceSessionView
	if code := eng.do("GET", base, nil, &v); code != 200 || v.State != "open" || v.IdleMinutes != 10 || len(v.Repos) != 1 || len(v.Envs) != 2 {
		t.Fatalf("trace = %d %+v", code, v)
	}
	if code := eng.do("POST", base+"/messages", TraceAskRequest{Text: ""}, nil); code != 400 {
		t.Fatalf("empty question = %d", code)
	}
	if code := eng.do("POST", base+"/messages", TraceAskRequest{Text: "kenapa?"}, &v); code != 202 || len(v.Messages) != 2 {
		t.Fatalf("ask = %d %+v", code, v)
	}
	for i := 0; i < 200 && v.Busy; i++ {
		time.Sleep(10 * time.Millisecond)
		eng.do("GET", base, nil, &v)
	}
	if v.Messages[1].Content != "Jawaban palsu untuk: kenapa?" || v.Messages[0].Username != "eng" {
		t.Fatalf("answer = %+v", v.Messages)
	}
	var refs TraceRefsResponse
	if code := eng.do("GET", base+"/refs?project=g/svc&q=ma", nil, &refs); code != 200 || len(refs.Deployments) != 2 || refs.Deployments[1].Commit != "sha-production" || len(refs.Branches) != 1 {
		t.Fatalf("refs = %d %+v", code, refs)
	}
	if code := eng.do("POST", base+"/retrace", TraceRetraceRequest{Project: "g/other", Env: "dev"}, nil); code != 400 {
		t.Fatalf("retrace unknown project = %d", code)
	}
	if code := eng.do("POST", base+"/retrace", TraceRetraceRequest{Project: "g/svc", Env: "dev"}, &v); code != 202 {
		t.Fatalf("retrace = %d", code)
	}
	for i := 0; i < 200 && v.Busy; i++ {
		time.Sleep(10 * time.Millisecond)
		eng.do("GET", base, nil, &v)
	}
	if ct := v.Messages[3].CodeTrace; ct == nil || ct.Commit != "sha-dev" || len(v.Repos) != 2 {
		t.Fatalf("retrace result = %+v", v)
	}
	if code := eng.do("POST", base+"/close", nil, &v); code != 200 || v.State != "closed" {
		t.Fatalf("close = %d %s", code, v.State)
	}
	if code := eng.do("POST", base+"/messages", TraceAskRequest{Text: "x"}, nil); code != 409 {
		t.Fatalf("ask on closed = %d", code)
	}
	if code := eng.do("POST", base+"/reopen", nil, &v); code != 200 || v.State != "open" {
		t.Fatalf("reopen = %d %s", code, v.State)
	}
}
