// Package tracechat lets engineers continue the Claude Code session of a
// code trace: ask questions about it, or trace the same error again in
// another version of the code. Each turn is a short-lived CLI process that
// resumes the session, so an idle session costs only disk space; it is
// shown as closed after a while, can be reopened, and its files are
// deleted after the retention period.
package tracechat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/analyzer"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/configrepo"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/redact"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

// Errors returned to the API.
var (
	ErrClosed     = errors.New("Sesi trace sudah ditutup. Buka lagi untuk melanjutkan.")
	ErrBusy       = errors.New("Sesi trace masih memproses pesan sebelumnya.")
	ErrBadRequest = errors.New("bad request")
)

// RepoSyncer is the subset of repos.Manager used here.
type RepoSyncer interface {
	Sync(ctx context.Context, container, ref string) (repos.Checkout, error)
	Prune(ctx context.Context, ttl time.Duration, keep func(dir string) bool) (int, error)
}

// GitLab is the subset of gitlab.Client used here.
type GitLab interface {
	DeployedCommit(ctx context.Context, project, env string, before time.Time) (gitlab.Deployment, error)
	ResolveRef(ctx context.Context, project, ref string) (string, error)
	Branches(ctx context.Context, project, search string) ([]gitlab.Branch, error)
}

// ConfigSource is the subset of configrepo.Source used here.
type ConfigSource interface {
	Checkout(ctx context.Context, env string, before time.Time) (configrepo.Checkout, error)
}

// Config tunes the service.
type Config struct {
	GitLabURL   string
	Envs        []string      // GitLab environments offered for a re-trace
	Idle        time.Duration // an open session without a message shows as closed after this
	Retention   time.Duration // session files are deleted after this long idle
	WorktreeTTL time.Duration
	Concurrency int // turns running at once, over all sessions
	// ClaudeDir is the CLI's config directory, where it keeps sessions
	// under projects/<cwd>/<session>.jsonl.
	ClaudeDir string
	// LogDir holds the jobs' log files (job-<id>.log), copied back into a
	// purged session's directory when it is reopened.
	LogDir string
}

// Service runs trace session turns in the background.
type Service struct {
	Store  *store.Store
	Tracer analyzer.Tracer
	Repos  RepoSyncer
	GitLab GitLab
	Cfg    Config
	// ConfigRepo, when set, adds the runtime JSON config of the chosen
	// environment to a re-trace.
	ConfigRepo ConfigSource
	// BaseCtx outlives requests; turns run under it.
	BaseCtx context.Context

	sem chan struct{}
}

// New returns a Service.
func New(st *store.Store, tr analyzer.Tracer, rs RepoSyncer, gl GitLab, cfg Config, base context.Context) *Service {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.ClaudeDir == "" {
		cfg.ClaudeDir = ClaudeConfigDir()
	}
	return &Service{Store: st, Tracer: tr, Repos: rs, GitLab: gl, Cfg: cfg, BaseCtx: base, sem: make(chan struct{}, cfg.Concurrency)}
}

// ClaudeConfigDir is where the Claude Code CLI keeps its state.
func ClaudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// View is a session as the web page shows it.
type View struct {
	State          string // open, closed or purged; open but idle shows as closed
	Busy           bool
	IdleClosed     bool // closed because nobody wrote for Cfg.Idle
	Repos          []store.TraceRepo
	Envs           []string
	Messages       []store.TraceMessage
	LastActivityAt time.Time
}

// Get returns a job's session, or store.ErrNotFound if it has none.
func (s *Service) Get(ctx context.Context, jobID int64) (View, error) {
	ts, err := s.Store.GetTraceSession(ctx, jobID)
	if err != nil {
		return View{}, err
	}
	msgs, err := s.Store.ListTraceMessages(ctx, jobID)
	if err != nil {
		return View{}, err
	}
	v := View{State: s.state(ts), Busy: ts.Busy, Repos: ts.Repos, Envs: s.Cfg.Envs, Messages: msgs, LastActivityAt: ts.LastActivityAt}
	v.IdleClosed = v.State == store.TraceClosed && ts.State == store.TraceOpen
	for i := range v.Repos {
		v.Repos[i].Dir = "" // server paths stay on the server
	}
	return v, nil
}

// state is the session state as users see it.
func (s *Service) state(ts store.TraceSession) string {
	if ts.State == store.TraceOpen && !ts.Busy && s.Cfg.Idle > 0 && time.Since(ts.LastActivityAt) > s.Cfg.Idle {
		return store.TraceClosed
	}
	return ts.State
}

// Close closes a session; Reopen opens it again.
func (s *Service) Close(ctx context.Context, jobID int64) error {
	return s.Store.SetTraceState(ctx, jobID, store.TraceClosed, "")
}

// Reopen opens a closed session. A purged one gets a new conversation that
// starts with a recap of the old one.
func (s *Service) Reopen(ctx context.Context, jobID int64) error {
	return s.Store.SetTraceState(ctx, jobID, store.TraceOpen, analyzer.NewSessionID())
}

// maxQuestion caps a question's length.
const maxQuestion = 4000

// Ask records a question and answers it in the background.
func (s *Service) Ask(ctx context.Context, jobID, userID int64, question string) error {
	question = strings.TrimSpace(question)
	if question == "" || len(question) > maxQuestion {
		return fmt.Errorf("%w: pertanyaan kosong atau lebih dari %d karakter", ErrBadRequest, maxQuestion)
	}
	if _, err := s.openSession(ctx, jobID); err != nil {
		return err
	}
	msgID, err := s.startTurn(ctx, jobID, userID, store.TraceKindChat, question)
	if err != nil {
		return err
	}
	go s.run(jobID, msgID, func(ctx context.Context, ts store.TraceSession, progress analyzer.Progress) store.TraceTurnResult {
		recap := ""
		if ts.Restart {
			recap = s.recap(ctx, jobID)
		}
		a, err := s.Tracer.Ask(ctx, session(ts), question, recap, analyzerRepos(ts.Repos), progress)
		if err != nil {
			return failed(ctx, err)
		}
		return store.TraceTurnResult{Content: redact.Sensitive(a.Text), Model: a.Model, Started: ts.Restart}
	})
	return nil
}

// RetraceRequest names the version to trace into: the commit deployed now
// to Env, or Ref (a branch, tag or commit).
type RetraceRequest struct {
	Project string
	Env     string
	Ref     string
}

// Retrace traces the error again in another version of one of the
// session's services, in the background.
func (s *Service) Retrace(ctx context.Context, jobID, userID int64, req RetraceRequest) error {
	ts, err := s.openSession(ctx, jobID)
	if err != nil {
		return err
	}
	var base *store.TraceRepo
	for i := range ts.Repos {
		if ts.Repos[i].Kind == "" && ts.Repos[i].Project == req.Project {
			base = &ts.Repos[i]
			break
		}
	}
	switch {
	case base == nil:
		return fmt.Errorf("%w: project %q tidak ada di sesi ini", ErrBadRequest, req.Project)
	case (req.Env == "") == (req.Ref == ""):
		return fmt.Errorf("%w: pilih satu environment atau branch", ErrBadRequest)
	case req.Env != "" && !contains(s.Cfg.Envs, req.Env):
		return fmt.Errorf("%w: environment %q tidak dikenal", ErrBadRequest, req.Env)
	case len(req.Ref) > 200:
		return fmt.Errorf("%w: ref terlalu panjang", ErrBadRequest)
	}
	label := "deploy terbaru di " + req.Env
	if req.Ref != "" {
		label = req.Ref
	}
	msgID, err := s.startTurn(ctx, jobID, userID, store.TraceKindRetrace, fmt.Sprintf("Trace ulang %s di %s", req.Project, label))
	if err != nil {
		return err
	}
	container := base.Container
	go s.run(jobID, msgID, func(ctx context.Context, ts store.TraceSession, progress analyzer.Progress) store.TraceTurnResult {
		progress("Mengambil kode " + label + " dari GitLab")
		ct := &store.CodeTrace{Project: req.Project, RefSource: store.RefManual, Env: req.Env}
		ref := req.Ref
		if req.Env != "" {
			dep, err := s.GitLab.DeployedCommit(ctx, req.Project, req.Env, time.Time{})
			if err != nil {
				return failed(ctx, fmt.Errorf("deployment %s: %w", req.Env, err))
			}
			ref, ct.Ref, ct.DeployJobURL = dep.SHA, dep.Ref, dep.JobURL
			if !dep.FinishedAt.IsZero() {
				ct.DeployedAt = dep.FinishedAt.UTC().Format(time.RFC3339)
			}
		} else {
			sha, err := s.GitLab.ResolveRef(ctx, req.Project, req.Ref)
			if errors.Is(err, gitlab.ErrNotFound) {
				return store.TraceTurnResult{Err: fmt.Sprintf("Branch, tag atau commit %q tidak ada di %s.", req.Ref, req.Project)}
			} else if err != nil {
				return failed(ctx, err)
			}
			ref, ct.Ref = sha, req.Ref
		}
		co, err := s.Repos.Sync(ctx, container, ref)
		if err != nil {
			return failed(ctx, err)
		}
		ct.Commit = co.Commit
		target := store.TraceRepo{Container: container, Project: req.Project, Dir: co.Dir, Commit: co.Commit, Ref: ct.Ref, RefSource: store.RefManual, Env: req.Env}
		all := withRepo(ts.Repos, target)
		if req.Env != "" && s.ConfigRepo != nil {
			// The config deployed to that environment now, like the code.
			progress("Mengambil config " + req.Env + " dari GitLab")
			cc, err := s.ConfigRepo.Checkout(ctx, req.Env, time.Time{})
			v := cc.Version
			if err != nil {
				v.Error = "Gagal mengambil config dari GitLab: " + redact.Sensitive(err.Error())
			} else {
				v.SetURL(s.Cfg.GitLabURL)
				all = withRepo(all, cc.Repo)
			}
			ct.Config = &v
		} else {
			ct.Config = lastConfig(ts.Repos, s.Cfg.GitLabURL)
		}
		recap := ""
		if ts.Restart {
			recap = s.recap(ctx, jobID)
		}
		t, err := s.Tracer.Retrace(ctx, session(ts), analyzerRepo(target), recap, analyzerRepos(all), progress)
		if err != nil {
			r := failed(ctx, err)
			r.Repos = all
			return r
		}
		ct.Status, ct.File, ct.Line, ct.Function, ct.Model = t.Status, t.File, t.Line, t.Function, t.Model
		ct.Snippet, ct.Explanation = redact.Sensitive(t.Snippet), redact.Sensitive(t.Explanation)
		ct.SetURL(s.Cfg.GitLabURL)
		return store.TraceTurnResult{Content: ct.Explanation, CodeTrace: ct, Model: t.Model, Repos: all, Started: ts.Restart}
	})
	return nil
}

func (s *Service) openSession(ctx context.Context, jobID int64) (store.TraceSession, error) {
	ts, err := s.Store.GetTraceSession(ctx, jobID)
	if err != nil {
		return ts, err
	}
	if ts.Busy {
		return ts, ErrBusy
	}
	if s.state(ts) != store.TraceOpen {
		return ts, ErrClosed
	}
	return ts, nil
}

func (s *Service) startTurn(ctx context.Context, jobID, userID int64, kind, text string) (int64, error) {
	id, err := s.Store.StartTraceTurn(ctx, jobID, userID, kind, text)
	if errors.Is(err, store.ErrTraceBusy) {
		return 0, ErrBusy
	}
	return id, err
}

// turnTimeout bounds one turn, including the wait for a free slot.
const turnTimeout = 20 * time.Minute

// run waits for a free slot, runs one turn and stores how it ended.
func (s *Service) run(jobID, msgID int64, turn func(ctx context.Context, ts store.TraceSession, progress analyzer.Progress) store.TraceTurnResult) {
	ctx, cancel := context.WithTimeout(s.BaseCtx, turnTimeout)
	defer cancel()
	log := slog.With("job", jobID, "trace_message", msgID)
	// Results are written even when ctx ends (shutdown, timeout).
	bg := context.WithoutCancel(ctx)
	finish := func(r store.TraceTurnResult) {
		if err := s.Store.FinishTraceTurn(bg, jobID, msgID, r); err != nil {
			log.Error("finish trace turn", "err", err)
		}
	}
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		finish(store.TraceTurnResult{Err: "Antrean sesi trace terlalu lama, coba lagi."})
		return
	}
	if err := s.Store.SetTraceRunning(bg, msgID); err != nil {
		log.Error("set trace turn running", "err", err)
	}
	ts, err := s.Store.GetTraceSession(bg, jobID)
	if err != nil {
		finish(store.TraceTurnResult{Err: "Sesi trace tidak ditemukan."})
		return
	}
	if ts.Restart {
		// Purging deleted the working directory; the new conversation gets
		// the logs back if they are still kept.
		err := analyzer.PrepareSession(ts.Dir, filepath.Join(s.Cfg.LogDir, fmt.Sprintf("job-%d.log", jobID)))
		if errors.Is(err, os.ErrNotExist) {
			err = os.MkdirAll(ts.Dir, 0o700)
		}
		if err != nil {
			finish(failed(ctx, err))
			return
		}
	}
	progress := func(line string) {
		if err := s.Store.AddTraceProgress(bg, msgID, line); err != nil {
			log.Warn("trace progress", "err", err)
		}
	}
	r := turn(ctx, ts, progress)
	if r.Err != "" {
		log.Warn("trace turn failed", "err", r.Err)
	} else {
		log.Info("trace turn done", "model", r.Model)
	}
	finish(r)
}

func failed(ctx context.Context, err error) store.TraceTurnResult {
	if cause := context.Cause(ctx); cause != nil && ctx.Err() != nil {
		return store.TraceTurnResult{Err: "Dihentikan: " + cause.Error()}
	}
	return store.TraceTurnResult{Err: "Gagal: " + redact.Sensitive(err.Error())}
}

func session(ts store.TraceSession) analyzer.Session {
	return analyzer.Session{ID: ts.SessionID, Dir: ts.Dir, Resume: !ts.Restart}
}

func analyzerRepo(r store.TraceRepo) analyzer.Repo {
	if r.Kind == store.RepoConfig {
		return configrepo.Repo(r)
	}
	return analyzer.Repo{Project: r.Project, Dir: r.Dir, Commit: r.Commit, Ref: r.Ref, Env: r.Env}
}

func analyzerRepos(rs []store.TraceRepo) []analyzer.Repo {
	out := make([]analyzer.Repo, len(rs))
	for i, r := range rs {
		out[i] = analyzerRepo(r)
	}
	return out
}

// lastConfig describes the most recent config checkout of a session, which
// a re-trace on a branch keeps reading; nil when there is none.
func lastConfig(rs []store.TraceRepo, gitlabURL string) *store.ConfigVersion {
	for i := len(rs) - 1; i >= 0; i-- {
		if r := rs[i]; r.Kind == store.RepoConfig {
			v := &store.ConfigVersion{Project: r.Project, Env: r.Env, Branch: r.Ref, Path: r.Path, Commit: r.Commit, Source: r.RefSource}
			v.SetURL(gitlabURL)
			return v
		}
	}
	return nil
}

// withRepo adds r to rs unless its checkout is already there.
func withRepo(rs []store.TraceRepo, r store.TraceRepo) []store.TraceRepo {
	out := append([]store.TraceRepo(nil), rs...)
	for _, x := range out {
		if x.Dir == r.Dir {
			return out
		}
	}
	return append(out, r)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// recapMessages and recapChars bound the recap of a restarted session.
const (
	recapMessages = 12
	recapChars    = 1500
)

// recap summarises the first trace and the latest messages of a session
// whose conversation was purged, for the new conversation.
func (s *Service) recap(ctx context.Context, jobID int64) string {
	var b strings.Builder
	if inv, err := s.Store.GetInvestigation(ctx, jobID); err == nil {
		fmt.Fprintf(&b, "Diagnosis: %s\n", clip(inv.Summary))
		if t := inv.CodeTrace; t != nil {
			fmt.Fprintf(&b, "First trace (%s @ %s): %s %s:%d — %s\n", t.Project, short(t.Commit), t.Status, t.File, t.Line, clip(t.Explanation))
		}
	}
	msgs, _ := s.Store.ListTraceMessages(ctx, jobID)
	if len(msgs) > recapMessages {
		msgs = msgs[len(msgs)-recapMessages:]
	}
	for _, m := range msgs {
		if m.Status != store.TraceMsgDone {
			continue
		}
		switch {
		case m.Role == store.TraceRoleUser:
			fmt.Fprintf(&b, "Engineer: %s\n", clip(m.Content))
		case m.CodeTrace != nil:
			t := m.CodeTrace
			fmt.Fprintf(&b, "Re-trace (%s @ %s): %s %s:%d — %s\n", t.Ref, short(t.Commit), t.Status, t.File, t.Line, clip(t.Explanation))
		default:
			fmt.Fprintf(&b, "You: %s\n", clip(m.Content))
		}
	}
	return strings.TrimSpace(b.String())
}

func clip(s string) string {
	if len(s) > recapChars {
		return s[:recapChars] + "…"
	}
	return s
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// Refs lists what a re-trace can target: the commit deployed now to each
// environment, and branches matching search.
type Refs struct {
	Deployments []EnvDeployment
	Branches    []gitlab.Branch
}

// EnvDeployment is the commit deployed now to an environment.
type EnvDeployment struct {
	Env        string
	Ref        string
	Commit     string
	FinishedAt time.Time
	Err        string
}

// Refs implements the re-trace picker.
func (s *Service) Refs(ctx context.Context, jobID int64, project, search string) (Refs, error) {
	ts, err := s.Store.GetTraceSession(ctx, jobID)
	if err != nil {
		return Refs{}, err
	}
	known := false
	for _, r := range ts.Repos {
		known = known || (r.Kind == "" && r.Project == project)
	}
	if !known {
		return Refs{}, fmt.Errorf("%w: project %q tidak ada di sesi ini", ErrBadRequest, project)
	}
	var out Refs
	type res struct {
		i int
		d EnvDeployment
	}
	ch := make(chan res, len(s.Cfg.Envs))
	for i, env := range s.Cfg.Envs {
		go func() {
			d := EnvDeployment{Env: env}
			dep, err := s.GitLab.DeployedCommit(ctx, project, env, time.Time{})
			if err != nil {
				d.Err = "Belum pernah di-deploy"
				if !errors.Is(err, gitlab.ErrNotFound) {
					d.Err = redact.Sensitive(err.Error())
				}
			} else {
				d.Ref, d.Commit, d.FinishedAt = dep.Ref, dep.SHA, dep.FinishedAt
			}
			ch <- res{i, d}
		}()
	}
	out.Deployments = make([]EnvDeployment, len(s.Cfg.Envs))
	for range s.Cfg.Envs {
		r := <-ch
		out.Deployments[r.i] = r.d
	}
	out.Branches, err = s.GitLab.Branches(ctx, project, search)
	if err != nil {
		return out, err
	}
	return out, nil
}

// Recover fails turns left running by a previous process.
func (s *Service) Recover(ctx context.Context) error {
	return s.Store.RecoverTraceTurns(ctx, "Server restart saat menjawab. Silakan kirim ulang.")
}

// Cleanup deletes the Claude Code files of sessions idle for longer than
// the retention period, then worktrees no remaining session uses.
func (s *Service) Cleanup(ctx context.Context) (sessions, worktrees int, err error) {
	if s.Cfg.Retention > 0 {
		stale, err := s.Store.StaleTraceSessions(ctx, time.Now().Add(-s.Cfg.Retention))
		if err != nil {
			return 0, 0, err
		}
		for _, ts := range stale {
			if err := s.purge(ctx, ts); err != nil {
				slog.Warn("purge trace session", "job", ts.JobID, "err", err)
				continue
			}
			sessions++
		}
	}
	if s.Repos == nil {
		return sessions, 0, nil
	}
	active, err := s.Store.ActiveTraceDirs(ctx)
	if err != nil {
		return sessions, 0, err
	}
	worktrees, err = s.Repos.Prune(ctx, s.Cfg.WorktreeTTL, func(dir string) bool { return active[dir] })
	return sessions, worktrees, err
}

func (s *Service) purge(ctx context.Context, ts store.TraceSession) error {
	if err := s.Store.SetTraceState(ctx, ts.JobID, store.TracePurged, ""); err != nil {
		return err
	}
	// The CLI files a session under projects/<cwd with / replaced>/; match
	// on the session ID instead of rebuilding that name.
	if ts.SessionID != "" && !strings.ContainsAny(ts.SessionID, "/*?[") {
		matches, _ := filepath.Glob(filepath.Join(s.Cfg.ClaudeDir, "projects", "*", ts.SessionID+"*"))
		for _, m := range matches {
			_ = os.RemoveAll(m)
		}
	}
	if ts.Dir != "" {
		return os.RemoveAll(ts.Dir)
	}
	return nil
}
