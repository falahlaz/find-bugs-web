package api

import (
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/vpn"
)

// LoginRequest is the login form.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// SessionResponse is returned by login and /api/me.
type SessionResponse struct {
	User      store.User `json:"user"`
	CSRFToken string     `json:"csrfToken" doc:"Send as X-CSRF-Token on POST/PATCH"`
}

// OKResponse is a bare acknowledgement.
type OKResponse struct {
	OK bool `json:"ok"`
}

// EnvironmentsResponse feeds the submit form.
type EnvironmentsResponse struct {
	Environments        []string `json:"environments"`
	TimeRanges          []string `json:"timeRanges"`
	DefaultTimeRange    string   `json:"defaultTimeRange"`
	Timezone            string   `json:"timezone" doc:"IANA zone for displaying times"`
	TransactionIDHeader string   `json:"transactionIdHeader" doc:"Header carrying the transaction ID in a pasted curl"`
}

// SubmitJobRequest creates an investigation.
type SubmitJobRequest struct {
	Environment string `json:"environment"`
	TimeRange   string `json:"timeRange" enum:"24h,48h"`
	Input       string `json:"input" doc:"Transaction ID or full curl command"`
	Force       bool   `json:"force,omitempty" doc:"Run again even if a result from the last 24h exists"`
}

// SubmitJobResponse is the queued job, or a previous result when Duplicate.
type SubmitJobResponse struct {
	Duplicate bool    `json:"duplicate"`
	Job       JobView `json:"job"`
}

// JobView is a job plus its result, shaped for the caller's role.
type JobView struct {
	store.Job
	QueuePosition *int    `json:"queuePosition,omitempty" doc:"Jobs ahead of this one while it is pending"`
	Result        *Result `json:"result,omitempty"`
}

// Result is the diagnosis. QA users get summary, severity and error source
// with a friendly message plus the redacted logs; engineers get every field.
type Result struct {
	Summary     string `json:"summary,omitempty"`
	Severity    string `json:"severity,omitempty" enum:"low,medium,high,critical"`
	ErrorSource string `json:"errorSource,omitempty" enum:"esb,tibco,internal,unknown"`
	SourceLabel string `json:"sourceLabel,omitempty"`
	QAMessage   string `json:"qaMessage,omitempty"`
	LLMFailed   bool   `json:"llmFailed"`
	// LinkedIDs are backend IDs the service logged this transaction under;
	// their logs were included in the analysis.
	LinkedIDs []string `json:"linkedIds,omitempty"`
	// SprintIdentifier is the X-SPRINT-IDENTIFIER header of the pasted curl.
	SprintIdentifier string `json:"sprintIdentifier,omitempty"`
	// Pods are the pods (<namespace>_<pod>) that logged the transaction.
	Pods            []string `json:"pods,omitempty"`
	ErrorType       string   `json:"errorType,omitempty"`
	FailedComponent string   `json:"failedComponent,omitempty"`
	LikelyCause     string   `json:"likelyCause,omitempty"`
	SuggestedAction string   `json:"suggestedAction,omitempty"`
	RelevantLogs    []string `json:"relevantLogs,omitempty"`
	RawLogSnippet   string   `json:"rawLogSnippet,omitempty"`
	// Model is the AI model(s) that produced the diagnosis (engineers only).
	Model string `json:"model,omitempty"`
	// CodeTrace is where an internal error was traced to in the service
	// code (engineers only).
	CodeTrace *store.CodeTrace `json:"codeTrace,omitempty"`
}

// JobListResponse is a page of jobs.
type JobListResponse struct {
	Jobs []store.Job `json:"jobs"`
}

// VPNSummary is the shared VPN state for banners.
type VPNSummary struct {
	Healthy     bool       `json:"healthy"`
	Detail      string     `json:"detail,omitempty"`
	CheckedAt   time.Time  `json:"checkedAt"`
	State       vpn.State  `json:"state"`
	Operator    string     `json:"operator,omitempty" doc:"User currently running a connect attempt"`
	ConnectedBy string     `json:"connectedBy,omitempty"`
	ConnectedAt *time.Time `json:"connectedAt,omitempty"`
}

// SplunkSummary is the shared Splunk session state.
type SplunkSummary struct {
	OK        bool               `json:"ok"`
	Paused    bool               `json:"paused"`
	Reauthing bool               `json:"reauthing"`
	Detail    string             `json:"detail,omitempty"`
	CheckedAt time.Time          `json:"checkedAt"`
	Session   splunk.SessionInfo `json:"session"`
}

// QueueSummary counts active jobs.
type QueueSummary struct {
	Active int `json:"active"`
	Max    int `json:"max"`
}

// Banner is a global message shown to every user.
type Banner struct {
	Level   string `json:"level" enum:"info,warning,error"`
	Message string `json:"message"`
	Action  string `json:"action,omitempty" enum:"vpn,splunk"`
}

// SystemStatus is polled by every page.
type SystemStatus struct {
	VPN     VPNSummary    `json:"vpn"`
	Splunk  SplunkSummary `json:"splunk"`
	Queue   QueueSummary  `json:"queue"`
	Banners []Banner      `json:"banners"`
}

// VPNStateResponse returns the connect state machine.
type VPNStateResponse struct {
	State vpn.State `json:"state"`
}

// LoginURLResponse is the SAML link once captured.
type LoginURLResponse struct {
	State vpn.State `json:"state"`
	URL   *string   `json:"url" doc:"/saml-login or an https URL; null until captured"`
}

// CallbackRequest carries the pasted globalprotectcallback: URI.
type CallbackRequest struct {
	URI string `json:"uri"`
}

// LogsResponse is redacted VPN log output.
type LogsResponse struct {
	Lines []string `json:"lines"`
}

// CreateUserRequest adds an account.
type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role" enum:"qa,engineer"`
	// Omitted means every menu.
	Menus []store.Menu `json:"menus,omitempty" doc:"Menus a QA can open; omitted = all"`
}

// UpdateUserRequest changes an account; omitted fields stay.
type UpdateUserRequest struct {
	Role     *string       `json:"role,omitempty" enum:"qa,engineer"`
	Active   *bool         `json:"active,omitempty"`
	Password *string       `json:"password,omitempty"`
	Menus    *[]store.Menu `json:"menus,omitempty" doc:"Replaces the menus a QA can open"`
}

// UserListResponse lists accounts.
type UserListResponse struct {
	Users []store.User `json:"users"`
}

// AuditListResponse lists audit entries.
type AuditListResponse struct {
	Entries []store.AuditEntry `json:"entries"`
}

// HealthResponse is the unauthenticated health check.
type HealthResponse struct {
	OK         bool `json:"ok"`
	DB         bool `json:"db"`
	VPNHealthy bool `json:"vpnHealthy"`
	SplunkOK   bool `json:"splunkOk"`
}

// TraceSessionView is the code trace chat of a job.
type TraceSessionView struct {
	State          string               `json:"state" enum:"open,closed,purged" doc:"open but idle sessions show as closed"`
	Busy           bool                 `json:"busy" doc:"A question or re-trace is being answered"`
	IdleClosed     bool                 `json:"idleClosed" doc:"Closed because nobody wrote for idleMinutes"`
	IdleMinutes    int                  `json:"idleMinutes"`
	LastActivityAt time.Time            `json:"lastActivityAt"`
	Repos          []TraceRepoView      `json:"repos" doc:"Checkouts the session can read"`
	Envs           []string             `json:"envs" doc:"GitLab environments offered for a re-trace"`
	Messages       []store.TraceMessage `json:"messages"`
}

// TraceRepoView is a checkout of a trace session.
type TraceRepoView struct {
	Project   string `json:"project"`
	Commit    string `json:"commit"`
	Ref       string `json:"ref"`
	RefSource string `json:"refSource" enum:"deployed,fallback,manual"`
	Env       string `json:"env,omitempty"`
}

// TraceAskRequest is a question about the trace.
type TraceAskRequest struct {
	Text string `json:"text"`
}

// TraceRetraceRequest asks for the error to be traced in another version:
// the commit deployed now to env, or ref (branch, tag or commit).
type TraceRetraceRequest struct {
	Project string `json:"project"`
	Env     string `json:"env,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

// TraceRefsResponse lists what a re-trace can target.
type TraceRefsResponse struct {
	Deployments []TraceEnvDeployment `json:"deployments"`
	Branches    []TraceBranch        `json:"branches"`
}

// TraceEnvDeployment is the commit deployed now to an environment.
type TraceEnvDeployment struct {
	Env        string     `json:"env"`
	Ref        string     `json:"ref,omitempty"`
	Commit     string     `json:"commit,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// TraceBranch is a branch and its tip commit.
type TraceBranch struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

// ReposResponse lists the repos under GITLAB_REPOS_DIR.
type ReposResponse struct {
	Repos        []RepoView      `json:"repos"`
	Clones       []RepoCloneView `json:"clones" doc:"Clones running or failed since the server started"`
	DefaultGroup string          `json:"defaultGroup" doc:"Group of a project given without one"`
	CanClone     bool            `json:"canClone" doc:"GitLab URL and token are set"`
	CanSession   bool            `json:"canSession" doc:"The rc-session script is installed"`
}

// RepoView is a repo and its Remote Control session, if any.
type RepoView struct {
	Project     string           `json:"project"`
	Branch      string           `json:"branch,omitempty" doc:"Empty when HEAD is detached (a tracer store)"`
	Commit      string           `json:"commit,omitempty"`
	Subject     string           `json:"subject,omitempty"`
	CommittedAt *time.Time       `json:"committedAt,omitempty"`
	Shallow     bool             `json:"shallow"`
	Session     *RepoSessionView `json:"session,omitempty"`
}

// RepoSessionView is a running `claude rc` session.
type RepoSessionView struct {
	Name  string    `json:"name" doc:"tmux session name"`
	URL   string    `json:"url,omitempty" doc:"claude.ai link once connected"`
	Since time.Time `json:"since"`
}

// RepoCloneView is a clone in progress or one that failed.
type RepoCloneView struct {
	Project   string    `json:"project"`
	State     string    `json:"state" enum:"cloning,failed"`
	Error     string    `json:"error,omitempty"`
	By        string    `json:"by"`
	StartedAt time.Time `json:"startedAt"`
}

// RepoRequest names a project: group/project, or a bare name in the
// default group.
type RepoRequest struct {
	Project string `json:"project"`
}

// RepoSessionResponse is a repo after starting or stopping its session.
type RepoSessionResponse struct {
	Repo   RepoView `json:"repo"`
	Output string   `json:"output" doc:"What the rc-session script reported"`
}

// ReportsResponse lists the trace-report knowledge base.
type ReportsResponse struct {
	Reports   []ReportView `json:"reports"`
	CanReadMD bool         `json:"canReadMd" doc:"Engineers read report.md and download the vault zip"`
}

// ReportView is one issue report from KNOWLEDGE_DIR.
type ReportView struct {
	Repo      string    `json:"repo"`
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Date      string    `json:"date,omitempty"`
	Status    string    `json:"status,omitempty"`
	Severity  string    `json:"severity,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Tags      []string  `json:"tags"`
	Endpoints []string  `json:"endpoints"`
	HasMD     bool      `json:"hasMd" doc:"Always false for QA"`
	HasHTML   bool      `json:"hasHtml"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ToolsResponse lists the MyTelkomsel support tools.
type ToolsResponse struct {
	Tools []tools.ToolInfo `json:"tools"`
}

// ToolInput is a tool's form values by field name.
type ToolInput map[string]any
