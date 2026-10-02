// Package worker runs queued investigations one at a time:
// VPN check → Splunk search → AI analysis → stored result.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/analyzer"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/redact"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/watchdog"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/notify"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

// RawSnippetChars is how much of the (redacted) raw log is kept for the
// engineer report, matching the bot's last-3000-characters snippet.
const RawSnippetChars = 3000

// Searcher is the subset of splunk.Client used by the worker.
type Searcher interface {
	Search(ctx context.Context, env, txn, timeRange string) (splunk.Result, error)
}

// Monitor is the subset of watchdog.Monitor used by the worker.
type Monitor interface {
	State() watchdog.State
	CheckVPN(ctx context.Context) bool
	CheckSplunk(ctx context.Context) bool
	MarkVPNDown(ctx context.Context, detail string)
	AutoReauth(ctx context.Context) bool
	Subscribe() <-chan struct{}
}

// Config tunes the worker.
type Config struct {
	WaitingExpiry time.Duration // WAITING_* jobs expire after this
	RecheckAfter  time.Duration // re-check VPN/Splunk if a job runs longer
	JobTimeout    time.Duration // hard limit per job
	IdlePoll      time.Duration // how often to look for work when idle
	LogDir        string        // per-job Splunk result files for the analyzer
	// CorrelationMaxIDs caps how many linked backend IDs are re-searched per
	// job (see package correlation); 0 turns the extra searches off.
	CorrelationMaxIDs int
	PublicURL         string
	// GitLabURL is the base for links to traced code.
	GitLabURL string
	// TraceMaxRepos caps how many service repos one trace reads.
	TraceMaxRepos int
	// EnvMap maps a Kubernetes namespace to the GitLab environment whose
	// deployed commit is traced.
	EnvMap map[string]string
	// SessionDir holds one working directory per traced job, kept so the
	// trace session can be continued.
	SessionDir string
}

// RepoSyncer is the subset of repos.Manager used by the worker.
type RepoSyncer interface {
	Project(container string) string
	DefaultRef() string
	Sync(ctx context.Context, container, ref string) (repos.Checkout, error)
}

// Deployments is the subset of gitlab.Client used by the worker.
type Deployments interface {
	DeployedCommit(ctx context.Context, project, env string, before time.Time) (gitlab.Deployment, error)
}

// Worker processes jobs.
type Worker struct {
	Store    *store.Store
	Splunk   Searcher
	Analyzer analyzer.Analyzer
	Monitor  Monitor
	Notify   notify.Notifier
	Cfg      Config
	// Repos and Tracer, when both set, trace internal errors to the code.
	Repos  RepoSyncer
	Tracer analyzer.Tracer
	// Deploys, when set, picks the commit deployed where the error happened
	// instead of the default branch.
	Deploys Deployments

	wake chan struct{}
}

// New returns a Worker.
func New(st *store.Store, s Searcher, a analyzer.Analyzer, m Monitor, n notify.Notifier, cfg Config) *Worker {
	if cfg.IdlePoll <= 0 {
		cfg.IdlePoll = 5 * time.Second
	}
	if n == nil {
		n = notify.Nop{}
	}
	if cfg.LogDir == "" {
		cfg.LogDir = "data/logs"
	}
	if cfg.SessionDir == "" {
		cfg.SessionDir = "data/trace-sessions"
	}
	return &Worker{Store: st, Splunk: s, Analyzer: a, Monitor: m, Notify: n, Cfg: cfg, wake: make(chan struct{}, 1)}
}

// Wake asks the worker to look for work now.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Recover fails jobs left running by a previous process. Call before Run.
func (w *Worker) Recover(ctx context.Context) error {
	failed, err := w.Store.FailRunning(ctx, "Server restart saat job berjalan. Silakan submit ulang.")
	for _, j := range failed {
		slog.Warn("job failed by restart", "job", j.ID)
	}
	return err
}

// Run loops until ctx is done.
func (w *Worker) Run(ctx context.Context) {
	monWake := w.Monitor.Subscribe()
	for ctx.Err() == nil {
		worked, err := w.Step(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("worker step", "err", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
		case <-w.wake:
		case <-monWake:
		case <-time.After(w.Cfg.IdlePoll):
		}
	}
}

// Step expires stale waiting jobs and processes at most one job. It reports
// whether a job was run (so the caller can immediately look for the next).
func (w *Worker) Step(ctx context.Context) (bool, error) {
	if w.Cfg.WaitingExpiry > 0 {
		expired, err := w.Store.ExpireWaiting(ctx, time.Now().Add(-w.Cfg.WaitingExpiry),
			fmt.Sprintf("Kedaluwarsa setelah menunggu %s. Sambungkan VPN/Splunk lalu submit ulang.", w.Cfg.WaitingExpiry))
		if err != nil {
			return false, err
		}
		for _, j := range expired {
			slog.Info("job expired", "job", j.ID)
		}
	}
	job, err := w.Store.NextPending(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	st := w.Monitor.State()
	if !st.VPNHealthy {
		return false, w.holdPending(ctx, store.StatusWaitingVPN)
	}
	if st.SplunkPaused {
		return false, w.holdPending(ctx, store.StatusWaitingSplunk)
	}
	w.run(ctx, job)
	return true, nil
}

// holdPending moves every QUEUED (or other WAITING) job to status so users
// see why nothing is moving.
func (w *Worker) holdPending(ctx context.Context, status string) error {
	from := []string{store.StatusQueued, store.StatusWaitingVPN, store.StatusWaitingSplunk}
	jobs, err := w.Store.ListJobs(ctx, store.JobFilter{Limit: 200})
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Status == status || !contains(from, j.Status) {
			continue
		}
		if err := w.Store.SetStatus(ctx, j.ID, from, store.Transition{Status: status}); err != nil && !errors.Is(err, store.ErrConflict) {
			return err
		}
	}
	return nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// errStop marks a failure already recorded on the job.
var errStop = errors.New("stop")

func (w *Worker) run(parent context.Context, job store.Job) {
	log := slog.With("job", job.ID, "txn", job.TransactionID, "env", job.Environment)
	err := w.Store.SetStatus(parent, job.ID, store.PendingStatuses, store.Transition{Status: store.StatusCheckingVPN, Stamp: "started_at"})
	if errors.Is(err, store.ErrConflict) {
		return // cancelled meanwhile
	}
	if err != nil {
		log.Error("start job", "err", err)
		return
	}

	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	if w.Cfg.JobTimeout > 0 {
		var c context.CancelFunc
		ctx, c = context.WithTimeoutCause(ctx, w.Cfg.JobTimeout, fmt.Errorf("job melebihi batas waktu %s", w.Cfg.JobTimeout))
		defer c()
	}
	if w.Cfg.RecheckAfter > 0 {
		stop := w.recheckLater(ctx, cancel, log)
		defer stop()
	}

	// 1. VPN, live.
	if !w.Monitor.CheckVPN(ctx) {
		// Nothing has run yet: put the job back to wait instead of failing it.
		w.set(parent, job.ID, store.Transition{Status: store.StatusWaitingVPN})
		return
	}

	// 2. Splunk.
	w.set(parent, job.ID, store.Transition{Status: store.StatusSearching, Stamp: "vpn_checked_at"})
	res, err := w.Splunk.Search(ctx, job.Environment, job.TransactionID, job.TimeRange)
	if errors.Is(err, splunk.ErrSessionExpired) || errors.Is(err, splunk.ErrNoSession) {
		log.Warn("splunk session expired during job", "err", err)
		if ctx.Err() == nil && w.Monitor.CheckVPN(ctx) && w.Monitor.AutoReauth(ctx) {
			res, err = w.Splunk.Search(ctx, job.Environment, job.TransactionID, job.TimeRange)
		}
	}
	if err != nil {
		w.fail(parent, ctx, job, w.searchFailure(parent, ctx, err))
		return
	}

	if res.Status == splunk.ResultNoLogs {
		if err := w.Store.SaveInvestigation(parent, store.Investigation{JobID: job.ID, RelevantLogs: []string{}}); err != nil {
			log.Error("save investigation", "err", err)
		}
		w.set(parent, job.ID, store.Transition{Status: store.StatusNoLogs, Stamp: "search_done_at"})
		return
	}

	// Some services log under their own backend _id; follow it.
	res, linked := w.followBackendIDs(ctx, log, job, res)

	// 3. AI analysis. Logs are redacted before they are stored or sent; the
	// analyzer reads every fetched event from the job's log file.
	w.set(parent, job.ID, store.Transition{Status: store.StatusAnalyzing, Stamp: "search_done_at"})
	logs := redact.Logs(res.Logs, 0)
	inv := store.Investigation{JobID: job.ID, RawLogSnippet: redact.Tail(logs, RawSnippetChars), RelevantLogs: []string{}, LinkedIDs: linked}
	log.Info("logs fetched", "events", len(res.Events), "matched", res.EventCount, "truncated", res.Truncated, "linked_ids", linked)
	var d analyzer.Diagnosis
	path, aerr := writeLogFile(w.Cfg.LogDir, job, res, linked)
	if aerr == nil {
		d, aerr = w.Analyzer.Analyze(ctx, job.TransactionID, path)
	}
	reason := ""
	if aerr != nil {
		if cause := context.Cause(ctx); cause != nil && ctx.Err() != nil {
			w.fail(parent, ctx, job, cause.Error())
			return
		}
		log.Warn("analysis failed, keeping raw logs", "err", aerr)
		inv.LLMFailed = true
		reason = "Analisis AI gagal; log mentah tetap tersedia untuk engineer."
		w.Notify.Notify(parent, "llm-failed", fmt.Sprintf("⚠️ Analisis AI gagal untuk job #%d (%s). Log mentah tersimpan.", job.ID, job.TransactionID))
	} else {
		inv.Summary, inv.ErrorType, inv.FailedComponent = d.Summary, d.ErrorType, d.FailedComponent
		inv.LikelyCause, inv.Severity, inv.SuggestedAction = d.LikelyCause, d.Severity, d.SuggestedAction
		inv.ErrorSource, inv.Model = d.ErrorSource, d.Model
		for _, l := range d.RelevantLogs {
			inv.RelevantLogs = append(inv.RelevantLogs, redact.Sensitive(l))
		}
		if d.ErrorSource == "internal" && w.Repos != nil && w.Tracer != nil {
			inv.CodeTrace = w.traceCode(ctx, log, job, path, d, res.Events)
		}
	}
	if err := w.Store.SaveInvestigation(parent, inv); err != nil {
		w.fail(parent, ctx, job, "Gagal menyimpan hasil: "+err.Error())
		return
	}
	w.set(parent, job.ID, store.Transition{Status: store.StatusDone, Stamp: "analyzed_at", FailureReason: reason})
	log.Info("job done", "llm_failed", inv.LLMFailed, "model", inv.Model)
}

// traceCode finds the code behind an internal error in the repos of the
// services that logged the transaction. It never fails the job: the
// diagnosis stands on its own and the trace says why it is missing.
func (w *Worker) traceCode(ctx context.Context, log *slog.Logger, job store.Job, logPath string, d analyzer.Diagnosis, events []splunk.Event) *store.CodeTrace {
	targets := repos.Targets(events)
	if len(targets) == 0 {
		return &store.CodeTrace{Status: store.TraceSkipped, Reason: "Log Splunk tidak menyebut container service-nya, jadi repo GitLab tidak bisa ditentukan."}
	}
	if n := w.Cfg.TraceMaxRepos; n > 0 && len(targets) > n {
		targets = targets[:n]
	}
	var rs []analyzer.Repo
	checkouts := map[string]Checkout{}
	var errs []string
	for _, tg := range targets {
		co, err := w.checkout(ctx, log, tg)
		if err != nil {
			log.Warn("repo sync failed", "container", tg.Container, "project", co.Project, "err", err)
			errs = append(errs, fmt.Sprintf("%s: %v", co.Project, err))
			continue
		}
		checkouts[co.Project] = co
		rs = append(rs, co.Repo())
	}
	if len(rs) == 0 {
		return &store.CodeTrace{Status: store.TraceFailed, Reason: "Gagal mengambil repo dari GitLab (cek VPN dan token): " + redact.Sensitive(strings.Join(errs, "; "))}
	}
	log.Info("tracing code", "repos", len(rs))
	sess := analyzer.Session{ID: analyzer.NewSessionID(), Dir: filepath.Join(w.Cfg.SessionDir, fmt.Sprintf("job-%d", job.ID))}
	if err := analyzer.PrepareSession(sess.Dir, logPath); err != nil {
		return &store.CodeTrace{Status: store.TraceFailed, Reason: "Gagal menyiapkan sesi trace: " + redact.Sensitive(err.Error())}
	}
	t, err := w.Tracer.Trace(ctx, sess, job.TransactionID, d, rs, nil)
	if err != nil {
		log.Warn("code trace failed", "err", err)
		reason := "Trace kode gagal: " + redact.Sensitive(err.Error())
		if cause := context.Cause(ctx); cause != nil && ctx.Err() != nil {
			reason = "Trace kode berhenti: " + cause.Error()
		}
		return &store.CodeTrace{Status: store.TraceFailed, Reason: reason}
	}
	ct := &store.CodeTrace{
		Status: t.Status, Project: t.Project, File: t.File, Line: t.Line, Function: t.Function,
		Snippet: redact.Sensitive(t.Snippet), Explanation: redact.Sensitive(t.Explanation), Model: t.Model,
	}
	if co, ok := checkouts[t.Project]; ok {
		co.Fill(ct, w.Cfg.GitLabURL)
	}
	// Keep the session so engineers can ask about the trace.
	ts := store.TraceSession{JobID: job.ID, SessionID: sess.ID, Dir: sess.Dir}
	for _, co := range checkouts {
		ts.Repos = append(ts.Repos, co.TraceRepo())
	}
	sort.Slice(ts.Repos, func(i, j int) bool { return ts.Repos[i].Project < ts.Repos[j].Project })
	if err := w.Store.SaveTraceSession(context.WithoutCancel(ctx), ts); err != nil {
		log.Warn("save trace session", "err", err)
	}
	log.Info("code traced", "status", ct.Status, "project", ct.Project, "file", ct.File, "line", ct.Line, "ref", ct.Ref, "ref_source", ct.RefSource)
	return ct
}

// Checkout is a synced repo plus how its version was chosen.
type Checkout struct {
	repos.Checkout
	RefSource string // store.RefDeployed, RefFallback or RefManual
	RefNote   string // why the default branch was used
	Env       string
	Deploy    *gitlab.Deployment
}

// Repo is the checkout as the tracer sees it.
func (co Checkout) Repo() analyzer.Repo {
	r := analyzer.Repo{Project: co.Project, Dir: co.Dir, Commit: co.Commit, Ref: co.Ref}
	if co.RefSource == store.RefDeployed {
		r.Env = co.Env
	}
	return r
}

// TraceRepo is the checkout as the trace session records it.
func (co Checkout) TraceRepo() store.TraceRepo {
	return store.TraceRepo{
		Container: co.Container, Project: co.Project, Dir: co.Dir, Commit: co.Commit, Ref: co.Ref,
		RefSource: co.RefSource, Env: co.Repo().Env,
	}
}

// Fill copies the version details into a trace and links it to GitLab.
func (co Checkout) Fill(ct *store.CodeTrace, gitlabURL string) {
	ct.Ref, ct.Commit, ct.RefSource, ct.RefNote, ct.Env = co.Ref, co.Commit, co.RefSource, co.RefNote, co.Env
	if co.Deploy != nil {
		if !co.Deploy.FinishedAt.IsZero() {
			ct.DeployedAt = co.Deploy.FinishedAt.UTC().Format(time.RFC3339)
		}
		ct.DeployJobURL = co.Deploy.JobURL
	}
	ct.SetURL(gitlabURL)
}

// checkout syncs the commit deployed in the target's environment when the
// error happened, falling back to the default branch when that is unknown.
func (w *Worker) checkout(ctx context.Context, log *slog.Logger, tg repos.Target) (Checkout, error) {
	project := w.Repos.Project(tg.Container)
	env := w.Cfg.EnvMap[tg.Namespace]
	var note string
	switch {
	case tg.Namespace == "":
		note = "Namespace pod tidak ada di log Splunk."
	case env == "":
		note = fmt.Sprintf("Namespace %s belum dipetakan ke environment GitLab (GITLAB_ENV_MAP).", tg.Namespace)
	case w.Deploys == nil:
		note = "Pencarian deployment GitLab tidak aktif."
	default:
		dep, err := w.Deploys.DeployedCommit(ctx, project, env, tg.LastSeen)
		if err == nil {
			co, err := w.Repos.Sync(ctx, tg.Container, dep.SHA)
			if err == nil {
				co.Ref = dep.Ref
				return Checkout{Checkout: co, RefSource: store.RefDeployed, Env: env, Deploy: &dep}, nil
			}
			log.Warn("deployed commit sync failed", "project", project, "env", env, "sha", dep.SHA, "err", err)
			note = fmt.Sprintf("Gagal mengambil commit %s yang ter-deploy di %s: %s", short(dep.SHA), env, redact.Sensitive(err.Error()))
		} else if errors.Is(err, gitlab.ErrNotFound) {
			note = fmt.Sprintf("Tidak ada job %s%s sukses sebelum waktu error.", gitlab.DeployJobPrefix, env)
		} else {
			log.Warn("deployment lookup failed", "project", project, "env", env, "err", err)
			note = "Gagal membaca deployment dari GitLab: " + redact.Sensitive(err.Error())
		}
	}
	co, err := w.Repos.Sync(ctx, tg.Container, "")
	return Checkout{Checkout: co, RefSource: store.RefFallback, RefNote: note, Env: env}, err
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// searchFailure turns a Splunk error into a user-facing reason, noting a
// VPN drop or an expired Splunk session.
func (w *Worker) searchFailure(parent, ctx context.Context, err error) string {
	if cause := context.Cause(ctx); cause != nil && ctx.Err() != nil {
		return cause.Error()
	}
	if errors.Is(err, splunk.ErrSessionExpired) || errors.Is(err, splunk.ErrNoSession) {
		return "Sesi Splunk kedaluwarsa dan login ulang gagal. Antrean dijeda sampai ada yang Re-auth di halaman Koneksi."
	}
	if !w.Monitor.CheckVPN(parent) {
		return "VPN putus saat mencari log di Splunk. Sambungkan VPN lalu submit ulang."
	}
	return "Gagal mencari log di Splunk: " + redact.Sensitive(err.Error())
}

func (w *Worker) fail(parent, ctx context.Context, job store.Job, reason string) {
	slog.Warn("job failed", "job", job.ID, "reason", reason)
	w.set(parent, job.ID, store.Transition{Status: store.StatusFailed, FailureReason: reason})
	link := ""
	if w.Cfg.PublicURL != "" {
		link = fmt.Sprintf("\n%s/jobs/%d", w.Cfg.PublicURL, job.ID)
	}
	w.Notify.Notify(parent, "", fmt.Sprintf("❌ Job #%d (%s, %s) gagal: %s%s", job.ID, job.TransactionID, job.Environment, reason, link))
}

func (w *Worker) set(ctx context.Context, id int64, t store.Transition) {
	if err := w.Store.SetStatus(ctx, id, nil, t); err != nil {
		slog.Error("set job status", "job", id, "status", t.Status, "err", err)
	}
}

// recheckLater re-validates VPN and Splunk once the job has been running for
// RecheckAfter; if either is gone it cancels the job so the user is not left
// waiting on a dead session.
func (w *Worker) recheckLater(ctx context.Context, cancel context.CancelCauseFunc, log *slog.Logger) func() {
	t := time.AfterFunc(w.Cfg.RecheckAfter, func() {
		if ctx.Err() != nil {
			return
		}
		// Status checks must not inherit the job context we may cancel.
		bg := context.WithoutCancel(ctx)
		if !w.Monitor.CheckVPN(bg) {
			log.Warn("vpn gone during long job")
			cancel(errors.New("Job berjalan lebih dari " + w.Cfg.RecheckAfter.String() + " dan VPN ternyata putus/sesi GlobalProtect habis. Login ulang VPN lalu submit ulang."))
			return
		}
		if st := w.Monitor.State(); !st.Reauthing && !w.Monitor.CheckSplunk(bg) {
			log.Warn("splunk session gone during long job")
			cancel(errors.New("Job berjalan lebih dari " + w.Cfg.RecheckAfter.String() + " dan sesi Splunk ternyata habis. Re-auth Splunk lalu submit ulang."))
		}
	})
	return func() { t.Stop() }
}
