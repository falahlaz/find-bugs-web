// Package api exposes the JSON HTTP API and serves the React build.
package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/jobs"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/mrtriage"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/rcsession"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/tracechat"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/usage"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/watchdog"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/worker"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/auth"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/config"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/vpn"
)

// VPN is the subset of vpn.Manager used by the API.
type VPN interface {
	Connect(by string) (vpn.State, error)
	LoginLink() (vpn.State, string)
	SAMLPage() ([]byte, bool)
	Callback(uri, by string) (vpn.CallbackResult, error)
	Disconnect() (vpn.CmdResult, error)
	Status() vpn.StatusResult
	State() vpn.State
	Logs() []string
}

// Splunk is the subset of splunk.Client used by the API.
type Splunk interface {
	Info() splunk.SessionInfo
}

// API wires handlers to services.
type API struct {
	Cfg     config.Config
	Store   *store.Store
	Auth    *auth.Service
	Jobs    *jobs.Service
	VPN     VPN
	Splunk  Splunk
	Monitor *watchdog.Monitor
	// Trace serves the code trace chat; nil when tracing is off.
	Trace *tracechat.Service
	// Repos lists and clones the repos under GITLAB_REPOS_DIR; nil
	// disables the Repo page. RC starts Remote Control sessions in them.
	Repos *repos.Manager
	RC    *rcsession.Manager
	// Tools runs the MyTelkomsel support tools; nil hides the Tools page.
	Tools *tools.Service
	// MRs triages the GitLab token owner's merge requests; nil hides the
	// MR Triage page.
	MRs *mrtriage.Service
	// Usage reports the Claude subscription limits; nil hides the page.
	Usage   *usage.Prober
	Web     fs.FS // React build (index.html + assets); nil in tests
	Version string
	// BaseCtx outlives requests (for background re-auth).
	BaseCtx context.Context
	// LogDir holds the per-job Splunk log files (job-<id>[.raw].log).
	LogDir string

	routes  []route
	cloning cloneState
}

type route struct {
	method, path, summary, tag, opID string
	public, raw                      bool
	roles                            []string
	// menu, if set, limits a QA to users granted that menu.
	menu  store.Menu
	query []string
	req   any
	resps map[int]any
	h     http.HandlerFunc
}

func (a *API) add(r route) { a.routes = append(a.routes, r) }

// Handler builds the HTTP handler.
func (a *API) Handler() http.Handler {
	if a.BaseCtx == nil {
		a.BaseCtx = context.Background()
	}
	a.routes = nil
	a.registerRoutes()
	mux := http.NewServeMux()
	for _, r := range a.routes {
		var h http.Handler = r.h
		if r.menu != "" {
			h = requireMenu(h, r.menu)
		}
		if !r.public {
			h = a.Auth.Require(h, r.roles...)
		}
		mux.Handle(r.method+" "+r.path, h)
	}
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, a.openAPI())
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Endpoint tidak ditemukan.")
	})
	mux.Handle("/", a.spa())
	return logRequests(securityHeaders(mux))
}

// OpenAPI returns the generated spec (for the `openapi` subcommand).
func (a *API) OpenAPI() map[string]any {
	a.routes = nil
	a.registerRoutes()
	return a.openAPI()
}

const engineer = store.RoleEngineer

// requireMenu runs after Auth.Require and rejects users without menu m.
func requireMenu(next http.Handler, m store.Menu) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !identity(r).User.HasMenu(m) {
			httpx.Error(w, http.StatusForbidden, "menu_forbidden", "Menu ini tidak dibuka untuk akun kamu. Minta Engineer untuk mengaktifkannya.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) registerRoutes() {
	ok := map[int]any{200: OKResponse{}}
	a.add(route{method: "GET", path: "/healthz", summary: "Health check", tag: "system", opID: "health", public: true,
		resps: map[int]any{200: HealthResponse{}}, h: a.health})

	a.add(route{method: "POST", path: "/api/auth/login", summary: "Log in", tag: "auth", opID: "login", public: true,
		req: LoginRequest{}, resps: map[int]any{200: SessionResponse{}}, h: a.login})
	a.add(route{method: "POST", path: "/api/auth/logout", summary: "Log out", tag: "auth", opID: "logout", resps: ok, h: a.logout})
	a.add(route{method: "GET", path: "/api/me", summary: "Current user", tag: "auth", opID: "me",
		resps: map[int]any{200: SessionResponse{}}, h: a.me})

	a.add(route{method: "GET", path: "/api/system/status", summary: "VPN, Splunk and queue state with banners", tag: "system", opID: "systemStatus",
		resps: map[int]any{200: SystemStatus{}}, h: a.systemStatus})
	a.add(route{method: "GET", path: "/api/environments", summary: "Form options", tag: "jobs", opID: "environments",
		resps: map[int]any{200: EnvironmentsResponse{}}, h: a.environments})

	a.add(route{method: "POST", path: "/api/jobs", menu: store.MenuInvestigasi, summary: "Submit an investigation", tag: "jobs", opID: "submitJob",
		req: SubmitJobRequest{}, resps: map[int]any{200: SubmitJobResponse{}, 202: SubmitJobResponse{}}, h: a.submitJob})
	a.add(route{method: "GET", path: "/api/jobs", menu: store.MenuInvestigasi, summary: "List jobs (QA: own only)", tag: "jobs", opID: "listJobs",
		query: []string{"environment", "status", "transactionId", "from", "to", "beforeId", "limit", "mine"},
		resps: map[int]any{200: JobListResponse{}}, h: a.listJobs})
	a.add(route{method: "GET", path: "/api/jobs/{id}", menu: store.MenuInvestigasi, summary: "Job detail and result", tag: "jobs", opID: "getJob",
		resps: map[int]any{200: JobView{}}, h: a.getJob})
	a.add(route{method: "GET", path: "/api/jobs/{id}/logs", menu: store.MenuInvestigasi, summary: "Download the job's unredacted Splunk log file", tag: "jobs",
		raw: true, h: a.downloadJobLogs})
	a.add(route{method: "POST", path: "/api/jobs/{id}/cancel", menu: store.MenuInvestigasi, summary: "Cancel a pending job", tag: "jobs", opID: "cancelJob",
		resps: map[int]any{200: JobView{}}, h: a.cancelJob})
	a.registerTraceRoutes()
	a.registerRepoRoutes()
	a.registerReportRoutes()
	a.registerToolRoutes()
	a.registerUsageRoutes()
	a.registerMRRoutes()

	a.add(route{method: "POST", path: "/api/vpn/connect", menu: store.MenuKoneksi, summary: "Start GlobalProtect connect", tag: "vpn", opID: "vpnConnect",
		resps: map[int]any{202: VPNStateResponse{}}, h: a.vpnConnect})
	a.add(route{method: "GET", path: "/api/vpn/login-url", menu: store.MenuKoneksi, summary: "SAML login link once captured", tag: "vpn", opID: "vpnLoginURL",
		resps: map[int]any{200: LoginURLResponse{}}, h: a.vpnLoginURL})
	a.add(route{method: "GET", path: "/saml-login", menu: store.MenuKoneksi, summary: "Captured SAML auto-submit page", tag: "vpn", raw: true, h: a.samlLogin})
	a.add(route{method: "POST", path: "/api/vpn/callback", menu: store.MenuKoneksi, summary: "Submit the globalprotectcallback: URI", tag: "vpn", opID: "vpnCallback",
		req: CallbackRequest{}, resps: map[int]any{200: vpn.CallbackResult{}}, h: a.vpnCallback})
	a.add(route{method: "POST", path: "/api/vpn/disconnect", menu: store.MenuKoneksi, summary: "Disconnect GlobalProtect", tag: "vpn", opID: "vpnDisconnect",
		resps: map[int]any{200: vpn.CmdResult{}}, h: a.vpnDisconnect})
	a.add(route{method: "GET", path: "/api/vpn/status", menu: store.MenuKoneksi, summary: "Live VPN status", tag: "vpn", opID: "vpnStatus",
		resps: map[int]any{200: vpn.StatusResult{}}, h: a.vpnStatus})
	a.add(route{method: "GET", path: "/api/vpn/logs", menu: store.MenuKoneksi, summary: "Redacted connect log", tag: "vpn", opID: "vpnLogs",
		resps: map[int]any{200: LogsResponse{}}, h: a.vpnLogs})

	a.add(route{method: "GET", path: "/api/splunk/status", menu: store.MenuKoneksi, summary: "Splunk session state", tag: "splunk", opID: "splunkStatus",
		resps: map[int]any{200: SplunkSummary{}}, h: a.splunkStatus})
	a.add(route{method: "POST", path: "/api/splunk/reauth", menu: store.MenuKoneksi, summary: "Start SSO re-auth (waits for 2FA in background)", tag: "splunk", opID: "splunkReauth",
		resps: map[int]any{202: SplunkSummary{}}, h: a.splunkReauth})

	a.add(route{method: "GET", path: "/api/users", summary: "List users", tag: "users", opID: "listUsers", roles: []string{engineer},
		resps: map[int]any{200: UserListResponse{}}, h: a.listUsers})
	a.add(route{method: "POST", path: "/api/users", summary: "Create user", tag: "users", opID: "createUser", roles: []string{engineer},
		req: CreateUserRequest{}, resps: map[int]any{201: store.User{}}, h: a.createUser})
	a.add(route{method: "PATCH", path: "/api/users/{id}", summary: "Update user", tag: "users", opID: "updateUser", roles: []string{engineer},
		req: UpdateUserRequest{}, resps: map[int]any{200: store.User{}}, h: a.updateUser})
	a.add(route{method: "GET", path: "/api/audit", summary: "Audit log of VPN/Splunk actions", tag: "users", opID: "listAudit", roles: []string{engineer}, query: []string{"limit"},
		resps: map[int]any{200: AuditListResponse{}}, h: a.listAudit})
}

func identity(r *http.Request) auth.Identity {
	id, _ := auth.FromContext(r.Context())
	return id
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad_id", "ID tidak valid.")
		return 0, false
	}
	return id, true
}

func (a *API) audit(r *http.Request, action, result, detail string) {
	if err := a.Store.AddAudit(r.Context(), identity(r).User.ID, action, result, detail); err != nil {
		slog.Error("audit", "err", err)
	}
}

// --- system ---

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dbOK := a.Store.Ping(ctx) == nil
	st := a.Monitor.State()
	code := http.StatusOK
	if !dbOK {
		code = http.StatusServiceUnavailable
	}
	httpx.JSON(w, code, HealthResponse{OK: dbOK, DB: dbOK, VPNHealthy: st.VPNHealthy, SplunkOK: st.SplunkOK})
}

func (a *API) systemStatus(w http.ResponseWriter, r *http.Request) {
	active, err := a.Store.CountActive(r.Context())
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a.buildStatus(active))
}

func (a *API) vpnSummary() VPNSummary {
	st := a.Monitor.State()
	s := VPNSummary{Healthy: st.VPNHealthy, Detail: st.VPNDetail, CheckedAt: st.VPNCheckedAt, State: a.VPN.State()}
	if op, ok := a.VPN.(interface {
		Snapshot() (string, string, *time.Time)
	}); ok {
		s.Operator, s.ConnectedBy, s.ConnectedAt = op.Snapshot()
	}
	return s
}

func (a *API) splunkSummary() SplunkSummary {
	st := a.Monitor.State()
	return SplunkSummary{OK: st.SplunkOK, Paused: st.SplunkPaused, Reauthing: st.Reauthing, Detail: st.SplunkDetail,
		CheckedAt: st.SplunkCheckedAt, Session: a.Splunk.Info()}
}

func (a *API) buildStatus(active int) SystemStatus {
	v, s := a.vpnSummary(), a.splunkSummary()
	banners := []Banner{}
	switch {
	case v.Operator != "":
		banners = append(banners, Banner{Level: "info", Action: "vpn", Message: v.Operator + " sedang menyambungkan VPN. Tunggu sebentar."})
	case !v.Healthy:
		banners = append(banners, Banner{Level: "error", Action: "vpn", Message: "VPN tidak tersambung. Job baru akan menunggu. Siapa pun bisa login ulang lewat halaman Koneksi."})
	case s.Reauthing:
		banners = append(banners, Banner{Level: "info", Action: "splunk", Message: "Login ulang Splunk sedang berjalan, menunggu approve 2FA di HP pemilik akun."})
	case s.Paused:
		banners = append(banners, Banner{Level: "error", Action: "splunk", Message: "Sesi Splunk kedaluwarsa. Antrean dijeda sampai ada yang klik Re-auth di halaman Koneksi."})
	}
	return SystemStatus{VPN: v, Splunk: s, Queue: QueueSummary{Active: active, Max: a.Cfg.QueueMax}, Banners: banners}
}

func (a *API) environments(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, EnvironmentsResponse{
		Environments: jobs.SortedEnvironments(a.Jobs.Environments), TimeRanges: jobs.TimeRanges, DefaultTimeRange: "24h",
		Timezone: a.Cfg.Location.String(), TransactionIDHeader: a.Jobs.Header,
	})
}

// --- auth ---

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	token, csrf, u, err := a.Auth.Login(r.Context(), strings.TrimSpace(req.Username), req.Password, auth.ClientIP(r))
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		httpx.Error(w, http.StatusTooManyRequests, "rate_limited", err.Error())
		return
	case errors.Is(err, auth.ErrBadCredentials):
		httpx.Error(w, http.StatusUnauthorized, "bad_credentials", err.Error())
		return
	case err != nil:
		httpx.Internal(w, r, err)
		return
	}
	a.Auth.SetCookie(w, token)
	httpx.JSON(w, http.StatusOK, SessionResponse{User: u, CSRFToken: csrf})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.Auth.Logout(r.Context(), r); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.Auth.ClearCookie(w)
	httpx.JSON(w, http.StatusOK, OKResponse{OK: true})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	id := identity(r)
	httpx.JSON(w, http.StatusOK, SessionResponse{User: id.User, CSRFToken: id.CSRF})
}

// --- jobs ---

func (a *API) submitJob(w http.ResponseWriter, r *http.Request) {
	var req SubmitJobRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	u := identity(r).User
	job, dup, err := a.Jobs.Submit(r.Context(), u, jobs.SubmitRequest{Environment: req.Environment, TimeRange: req.TimeRange, Input: req.Input, Force: req.Force})
	var ve *jobs.ValidationError
	var le *jobs.LimitError
	switch {
	case errors.As(err, &ve):
		httpx.Error(w, http.StatusBadRequest, "invalid_input", ve.Msg)
		return
	case errors.As(err, &le):
		httpx.Error(w, http.StatusConflict, "queue_limit", le.Msg)
		return
	case err != nil:
		httpx.Internal(w, r, err)
		return
	}
	view, err := a.jobView(r.Context(), u, job)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	code := http.StatusAccepted
	if dup {
		code = http.StatusOK
	}
	httpx.JSON(w, code, SubmitJobResponse{Duplicate: dup, Job: view})
}

func (a *API) listJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.JobFilter{Environment: q.Get("environment"), Status: q.Get("status"), TransactionID: q.Get("transactionId")}
	if q.Get("mine") == "1" {
		f.UserID = identity(r).User.ID
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.BeforeID, _ = strconv.ParseInt(q.Get("beforeId"), 10, 64)
	for key, dst := range map[string]**time.Time{"from": &f.From, "to": &f.To} {
		if v := q.Get(key); v != "" {
			t, err := parseDate(v, a.Cfg.Location, key == "to")
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, "bad_date", key+" harus YYYY-MM-DD atau RFC3339.")
				return
			}
			*dst = &t
		}
	}
	list, err := a.Jobs.List(r.Context(), identity(r).User, f)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, JobListResponse{Jobs: list})
}

// parseDate accepts RFC3339 or a local date; a date for "to" means the end of that day.
func parseDate(v string, loc *time.Location, end bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation(time.DateOnly, v, loc)
	if err != nil {
		return t, err
	}
	if end {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}

func (a *API) getJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := identity(r).User
	j, err := a.Jobs.Get(r.Context(), u, id)
	if errors.Is(err, jobs.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	view, err := a.jobView(r.Context(), u, j)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, view)
}

// downloadJobLogs serves the job's unredacted Splunk log file as an
// attachment. Files are purged after RETENTION_LOG_FILE_DAYS.
func (a *API) downloadJobLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	j, err := a.Jobs.Get(r.Context(), identity(r).User, id)
	if errors.Is(err, jobs.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	f, err := os.Open(worker.RawLogFilePath(a.LogDir, id))
	if errors.Is(err, os.ErrNotExist) {
		httpx.Error(w, http.StatusNotFound, "logs_expired", "Log sudah tidak tersedia (disimpan maksimal 3 hari) atau job ini tidak punya log.")
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.audit(r, "download_logs", "ok", fmt.Sprintf("job #%d", id))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="job-%d-%s.log"`, id, j.TransactionID))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func (a *API) cancelJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := identity(r).User
	j, err := a.Jobs.Cancel(r.Context(), u, id)
	switch {
	case errors.Is(err, jobs.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not_found", err.Error())
		return
	case errors.Is(err, jobs.ErrForbidden):
		httpx.Error(w, http.StatusForbidden, "forbidden", err.Error())
		return
	case errors.Is(err, jobs.ErrNotPending):
		httpx.Error(w, http.StatusConflict, "not_pending", err.Error())
		return
	case err != nil:
		httpx.Internal(w, r, err)
		return
	}
	view, err := a.jobView(r.Context(), u, j)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, view)
}

func (a *API) jobView(ctx context.Context, u store.User, j store.Job) (JobView, error) {
	v := JobView{Job: j}
	if !store.IsFinal(j.Status) {
		pos, err := a.Store.QueuePosition(ctx, j.ID)
		if err != nil {
			return v, err
		}
		v.QueuePosition = &pos
		return v, nil
	}
	inv, err := a.Store.GetInvestigation(ctx, j.ID)
	if errors.Is(err, store.ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	v.Result = shapeResult(u, j, inv)
	return v, nil
}

// shapeResult mirrors find-bugs-bot bot/formatter.py: QA gets the summary,
// severity, a message based on the error source and the redacted logs (the
// unredacted log file is served by downloadJobLogs); engineers get everything.
func shapeResult(u store.User, j store.Job, inv store.Investigation) *Result {
	label, msg := sourceText(inv.ErrorSource)
	res := &Result{Summary: inv.Summary, Severity: inv.Severity, ErrorSource: inv.ErrorSource, SourceLabel: label, QAMessage: msg, LLMFailed: inv.LLMFailed, LinkedIDs: inv.LinkedIDs,
		SprintIdentifier: j.SprintIdentifier, Pods: inv.Pods, RelevantLogs: inv.RelevantLogs, RawLogSnippet: inv.RawLogSnippet}
	if inv.LLMFailed {
		res.QAMessage = "Tim engineering sedang meninjau log secara manual dan akan menindaklanjuti."
	}
	if u.Role == store.RoleEngineer {
		res.ErrorType, res.FailedComponent, res.LikelyCause = inv.ErrorType, inv.FailedComponent, inv.LikelyCause
		res.SuggestedAction = inv.SuggestedAction
		res.Model, res.CodeTrace = inv.Model, inv.CodeTrace
	}
	return res
}

func sourceText(src string) (string, string) {
	switch strings.ToLower(src) {
	case "esb":
		return "ESB (External)", "Error berasal dari ESB (sistem eksternal). Tim engineering akan berkoordinasi dengan tim ESB."
	case "tibco":
		return "TIBCO (External)", "Error berasal dari TIBCO (sistem eksternal). Tim engineering akan berkoordinasi dengan tim terkait."
	case "internal":
		return "Internal", "Error berasal dari service internal. Tim engineering sudah diberi tahu dan akan menindaklanjuti."
	case "":
		return "", ""
	}
	return "Unknown", "Tim engineering sudah diberi tahu dan sedang menelusurinya."
}

// --- VPN ---

func (a *API) vpnErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, vpn.ErrBusy), errors.Is(err, vpn.ErrBadState):
		httpx.Error(w, http.StatusConflict, "vpn_busy", vpnMessage(err, a.vpnSummary()))
	case errors.Is(err, vpn.ErrInvalid):
		httpx.Error(w, http.StatusBadRequest, "vpn_invalid", err.Error())
	default:
		httpx.Internal(w, r, err)
	}
}

func vpnMessage(err error, s VPNSummary) string {
	if s.Operator != "" {
		return s.Operator + " sedang menyambungkan VPN. Tunggu sampai selesai."
	}
	return err.Error()
}

func (a *API) vpnConnect(w http.ResponseWriter, r *http.Request) {
	u := identity(r).User
	st, err := a.VPN.Connect(u.Username)
	if err != nil {
		a.audit(r, "vpn.connect", "error", err.Error())
		a.vpnErr(w, r, err)
		return
	}
	a.audit(r, "vpn.connect", "started", "")
	httpx.JSON(w, http.StatusAccepted, VPNStateResponse{State: st})
}

func (a *API) vpnLoginURL(w http.ResponseWriter, _ *http.Request) {
	st, link := a.VPN.LoginLink()
	resp := LoginURLResponse{State: st}
	if link != "" {
		resp.URL = &link
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (a *API) samlLogin(w http.ResponseWriter, r *http.Request) {
	b, ok := a.VPN.SAMLPage()
	if !ok {
		http.NotFound(w, r)
		return
	}
	// GP's page is an inline auto-submitting form to the IdP; relax the CSP for it only.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; form-action https:; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func (a *API) vpnCallback(w http.ResponseWriter, r *http.Request) {
	var req CallbackRequest
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if !httpx.Decode(w, r, &req) {
		return
	}
	res, err := a.VPN.Callback(strings.TrimSpace(req.URI), identity(r).User.Username)
	if err != nil {
		a.audit(r, "vpn.callback", "error", err.Error())
		a.vpnErr(w, r, err)
		return
	}
	result := "connected"
	if res.State != vpn.StateConnected {
		result = "failed"
	}
	a.audit(r, "vpn.callback", result, "exit="+strconv.Itoa(res.ExitCode))
	if res.State == vpn.StateConnected {
		go a.Monitor.CheckVPN(context.WithoutCancel(a.BaseCtx))
	}
	httpx.JSON(w, http.StatusOK, res)
}

func (a *API) vpnDisconnect(w http.ResponseWriter, r *http.Request) {
	res, err := a.VPN.Disconnect()
	if err != nil {
		a.vpnErr(w, r, err)
		return
	}
	a.audit(r, "vpn.disconnect", "ok", "exit="+strconv.Itoa(res.ExitCode))
	go a.Monitor.CheckVPN(context.WithoutCancel(a.BaseCtx))
	httpx.JSON(w, http.StatusOK, res)
}

func (a *API) vpnStatus(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, a.Monitor.VPNStatus(context.WithoutCancel(r.Context())))
}

func (a *API) vpnLogs(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, LogsResponse{Lines: a.VPN.Logs()})
}

// --- Splunk ---

func (a *API) splunkStatus(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, a.splunkSummary())
}

func (a *API) splunkReauth(w http.ResponseWriter, r *http.Request) {
	st := a.Monitor.State()
	if !st.VPNHealthy {
		httpx.Error(w, http.StatusConflict, "vpn_down", "VPN belum tersambung. Sambungkan VPN dulu.")
		return
	}
	if st.Reauthing {
		httpx.Error(w, http.StatusConflict, "reauth_running", "Login ulang Splunk sedang berjalan.")
		return
	}
	uid := identity(r).User.ID
	a.audit(r, "splunk.reauth", "started", "")
	go func() {
		ctx := context.WithoutCancel(a.BaseCtx)
		err := a.Monitor.Reauth(ctx)
		result, detail := "ok", ""
		if err != nil {
			result, detail = "error", err.Error()
		}
		if aerr := a.Store.AddAudit(ctx, uid, "splunk.reauth", result, detail); aerr != nil {
			slog.Error("audit", "err", aerr)
		}
	}()
	// Give the goroutine a moment to flip Reauthing so the response reflects it.
	time.Sleep(50 * time.Millisecond)
	httpx.JSON(w, http.StatusAccepted, a.splunkSummary())
}

// --- users ---

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.Store.ListUsers(r.Context())
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, UserListResponse{Users: users})
}

func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if !validUsername(req.Username) {
		httpx.Error(w, http.StatusBadRequest, "bad_username", "Username 3–64 karakter: huruf, angka, '.', '_' atau '-'.")
		return
	}
	if !store.ValidRole(req.Role) {
		httpx.Error(w, http.StatusBadRequest, "bad_role", "Role harus qa atau engineer.")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_password", err.Error())
		return
	}
	menus, ok := store.NormalizeMenus(req.Menus)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad_menu", "Menu tidak dikenal.")
		return
	}
	u, err := a.Store.CreateUser(r.Context(), req.Username, hash, req.Role)
	if errors.Is(err, store.ErrDuplicate) {
		httpx.Error(w, http.StatusConflict, "duplicate", "Username sudah dipakai.")
		return
	}
	if err == nil && req.Menus != nil {
		u, err = a.Store.UpdateUser(r.Context(), u.ID, store.UserUpdate{Menus: &menus})
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.audit(r, "user.create", "ok", u.Username+" ("+u.Role+")"+menuDetail(u))
	httpx.JSON(w, http.StatusCreated, u)
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req UpdateUserRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	me := identity(r).User
	if id == me.ID && ((req.Active != nil && !*req.Active) || (req.Role != nil && *req.Role != store.RoleEngineer)) {
		httpx.Error(w, http.StatusBadRequest, "self_lockout", "Tidak bisa menonaktifkan atau menurunkan role akun sendiri.")
		return
	}
	upd := store.UserUpdate{Role: req.Role, Active: req.Active}
	if req.Role != nil && !store.ValidRole(*req.Role) {
		httpx.Error(w, http.StatusBadRequest, "bad_role", "Role harus qa atau engineer.")
		return
	}
	if req.Menus != nil {
		menus, ok := store.NormalizeMenus(*req.Menus)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, "bad_menu", "Menu tidak dikenal.")
			return
		}
		upd.Menus = &menus
	}
	if req.Password != nil {
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "bad_password", err.Error())
			return
		}
		upd.PasswordHash = &hash
	}
	u, err := a.Store.UpdateUser(r.Context(), id, upd)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not_found", "User tidak ditemukan.")
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	// Any change to role, status or password ends existing sessions. Menus
	// are checked on every request, so a menu change needs no new login.
	if req.Role != nil || req.Active != nil || req.Password != nil {
		if err := a.Store.DeleteUserSessions(r.Context(), id); err != nil {
			slog.Error("delete sessions", "err", err)
		}
	}
	var changes []string
	if req.Role != nil {
		changes = append(changes, "role → "+*req.Role)
	}
	if req.Active != nil {
		changes = append(changes, map[bool]string{true: "diaktifkan", false: "dinonaktifkan"}[*req.Active])
	}
	if req.Password != nil {
		changes = append(changes, "password direset")
	}
	if req.Menus != nil {
		changes = append(changes, "menu → "+menuList(u.Menus))
	}
	a.audit(r, "user.update", "ok", u.Username+": "+strings.Join(changes, ", "))
	httpx.JSON(w, http.StatusOK, u)
}

func menuList(ms []store.Menu) string {
	if len(ms) == 0 {
		return "(tidak ada)"
	}
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = string(m)
	}
	return strings.Join(names, ", ")
}

// menuDetail describes a QA's menus for the audit log.
func menuDetail(u store.User) string {
	if u.Role == store.RoleEngineer {
		return ""
	}
	return " menu: " + menuList(u.Menus)
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := a.Store.ListAudit(r.Context(), limit)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, AuditListResponse{Entries: entries})
}
