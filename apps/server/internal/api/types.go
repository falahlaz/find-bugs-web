package api

import (
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
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
	Environments     []string `json:"environments"`
	TimeRanges       []string `json:"timeRanges"`
	DefaultTimeRange string   `json:"defaultTimeRange"`
	Timezone         string   `json:"timezone" doc:"IANA zone for displaying times"`
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
// with a friendly message; engineers get every field and the raw log tail.
type Result struct {
	Summary     string `json:"summary,omitempty"`
	Severity    string `json:"severity,omitempty" enum:"low,medium,high,critical"`
	ErrorSource string `json:"errorSource,omitempty" enum:"esb,tibco,internal,unknown"`
	SourceLabel string `json:"sourceLabel,omitempty"`
	QAMessage   string `json:"qaMessage,omitempty"`
	LLMFailed   bool   `json:"llmFailed"`
	// LinkedIDs are backend IDs the service logged this transaction under;
	// their logs were included in the analysis.
	LinkedIDs       []string `json:"linkedIds,omitempty"`
	ErrorType       string   `json:"errorType,omitempty"`
	FailedComponent string   `json:"failedComponent,omitempty"`
	LikelyCause     string   `json:"likelyCause,omitempty"`
	SuggestedAction string   `json:"suggestedAction,omitempty"`
	RelevantLogs    []string `json:"relevantLogs,omitempty"`
	RawLogSnippet   string   `json:"rawLogSnippet,omitempty"`
	// Model is the AI model(s) that produced the diagnosis (engineers only).
	Model string `json:"model,omitempty"`
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
}

// UpdateUserRequest changes an account; omitted fields stay.
type UpdateUserRequest struct {
	Role     *string `json:"role,omitempty" enum:"qa,engineer"`
	Active   *bool   `json:"active,omitempty"`
	Password *string `json:"password,omitempty"`
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
