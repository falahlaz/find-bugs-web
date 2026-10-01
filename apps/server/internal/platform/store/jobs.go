package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Job statuses.
const (
	StatusQueued        = "QUEUED"
	StatusCheckingVPN   = "CHECKING_VPN"
	StatusSearching     = "SEARCHING"
	StatusAnalyzing     = "ANALYZING"
	StatusWaitingVPN    = "WAITING_VPN"
	StatusWaitingSplunk = "WAITING_SPLUNK"
	StatusDone          = "DONE"
	StatusNoLogs        = "NO_LOGS"
	StatusFailed        = "FAILED"
	StatusCancelled     = "CANCELLED"
	StatusExpired       = "EXPIRED"
)

// ActiveStatuses count against the queue limits.
var ActiveStatuses = []string{StatusQueued, StatusWaitingVPN, StatusWaitingSplunk, StatusCheckingVPN, StatusSearching, StatusAnalyzing}

// RunningStatuses are owned by the worker.
var RunningStatuses = []string{StatusCheckingVPN, StatusSearching, StatusAnalyzing}

// PendingStatuses wait for the worker to pick them up.
var PendingStatuses = []string{StatusQueued, StatusWaitingVPN, StatusWaitingSplunk}

// IsFinal reports whether status is terminal.
func IsFinal(status string) bool {
	switch status {
	case StatusDone, StatusNoLogs, StatusFailed, StatusCancelled, StatusExpired:
		return true
	}
	return false
}

func inList(n int) string { return "(" + strings.TrimSuffix(strings.Repeat("?,", n), ",") + ")" }

func strArgs(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// Job is one investigation request.
type Job struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"userId"`
	Username      string     `json:"username"`
	TransactionID string     `json:"transactionId"`
	Environment   string     `json:"environment"`
	TimeRange     string     `json:"timeRange"`
	InputKind     string     `json:"inputKind" enum:"transaction_id,curl"`
	Status        string     `json:"status" enum:"QUEUED,CHECKING_VPN,SEARCHING,ANALYZING,WAITING_VPN,WAITING_SPLUNK,DONE,NO_LOGS,FAILED,CANCELLED,EXPIRED"`
	FailureReason string     `json:"failureReason,omitempty"`
	QueuedAt      time.Time  `json:"queuedAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	VPNCheckedAt  *time.Time `json:"vpnCheckedAt,omitempty"`
	SearchDoneAt  *time.Time `json:"searchDoneAt,omitempty"`
	AnalyzedAt    *time.Time `json:"analyzedAt,omitempty"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
	WaitingSince  *time.Time `json:"waitingSince,omitempty"`
}

const jobCols = `j.id, j.user_id, u.username, j.transaction_id, j.environment, j.time_range, j.input_kind, j.status,
	j.failure_reason, j.queued_at, j.started_at, j.vpn_checked_at, j.search_done_at, j.analyzed_at, j.finished_at, j.waiting_since`

const jobFrom = ` FROM jobs j JOIN users u ON u.id = j.user_id`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var reason sql.NullString
	var queued string
	var started, vpn, search, analyzed, finished, waiting sql.NullString
	err := row.Scan(&j.ID, &j.UserID, &j.Username, &j.TransactionID, &j.Environment, &j.TimeRange, &j.InputKind, &j.Status,
		&reason, &queued, &started, &vpn, &search, &analyzed, &finished, &waiting)
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	if err != nil {
		return j, err
	}
	j.FailureReason = reason.String
	j.QueuedAt = parseTS(queued)
	j.StartedAt, j.VPNCheckedAt, j.SearchDoneAt = parseNullTS(started), parseNullTS(vpn), parseNullTS(search)
	j.AnalyzedAt, j.FinishedAt, j.WaitingSince = parseNullTS(analyzed), parseNullTS(finished), parseNullTS(waiting)
	return j, nil
}

// NewJob holds the fields of a job to enqueue.
type NewJob struct {
	UserID        int64
	TransactionID string
	Environment   string
	TimeRange     string
	InputKind     string
}

// Limits for EnqueueJob.
type Limits struct {
	Total   int
	PerUser int
}

// Queue-full errors returned by EnqueueJob.
var (
	ErrQueueFull   = errors.New("queue full")
	ErrUserLimited = errors.New("user job limit reached")
)

// EnqueueJob inserts a QUEUED job if the queue limits allow it. The check and
// insert run in one transaction so concurrent submits cannot overshoot.
func (s *Store) EnqueueJob(ctx context.Context, nj NewJob, lim Limits) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var total, mine int
	q := `SELECT COUNT(*), COALESCE(SUM(user_id = ?), 0) FROM jobs WHERE status IN ` + inList(len(ActiveStatuses))
	if err := tx.QueryRowContext(ctx, q, append([]any{nj.UserID}, strArgs(ActiveStatuses)...)...).Scan(&total, &mine); err != nil {
		return Job{}, err
	}
	if mine >= lim.PerUser {
		return Job{}, ErrUserLimited
	}
	if total >= lim.Total {
		return Job{}, ErrQueueFull
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO jobs (user_id, transaction_id, environment, time_range, input_kind, status, queued_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nj.UserID, nj.TransactionID, nj.Environment, nj.TimeRange, nj.InputKind, StatusQueued, ts(s.now()))
	if err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetJob(ctx, id)
}

// GetJob loads one job.
func (s *Store) GetJob(ctx context.Context, id int64) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobCols+jobFrom+` WHERE j.id = ?`, id))
}

// JobFilter narrows ListJobs. Zero values mean "any".
type JobFilter struct {
	UserID        int64
	Environment   string
	Status        string
	TransactionID string
	From, To      *time.Time
	Limit         int
	BeforeID      int64
}

// ListJobs returns jobs newest first.
func (s *Store) ListJobs(ctx context.Context, f JobFilter) ([]Job, error) {
	var where []string
	var args []any
	add := func(cond string, a any) { where, args = append(where, cond), append(args, a) }
	if f.UserID != 0 {
		add("j.user_id = ?", f.UserID)
	}
	if f.Environment != "" {
		add("j.environment = ?", f.Environment)
	}
	if f.Status != "" {
		add("j.status = ?", f.Status)
	}
	if f.TransactionID != "" {
		add("j.transaction_id LIKE ? ESCAPE '\\'", "%"+escapeLike(f.TransactionID)+"%")
	}
	if f.From != nil {
		add("j.queued_at >= ?", ts(*f.From))
	}
	if f.To != nil {
		add("j.queued_at < ?", ts(*f.To))
	}
	if f.BeforeID != 0 {
		add("j.id < ?", f.BeforeID)
	}
	q := `SELECT ` + jobCols + jobFrom
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q += ` ORDER BY j.id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// FindRecentResult returns the newest DONE job for the same parameters queued
// at or after since.
func (s *Store) FindRecentResult(ctx context.Context, txn, env, timeRange string, since time.Time) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobCols+jobFrom+`
		WHERE j.transaction_id = ? AND j.environment = ? AND j.time_range = ? AND j.status = ? AND j.queued_at >= ?
		ORDER BY j.id DESC LIMIT 1`, txn, env, timeRange, StatusDone, ts(since)))
}

// NextPending returns the oldest job waiting for the worker.
func (s *Store) NextPending(ctx context.Context) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobCols+jobFrom+` WHERE j.status IN `+inList(len(PendingStatuses))+` ORDER BY j.id LIMIT 1`,
		strArgs(PendingStatuses)...))
}

// QueuePosition returns how many pending or running jobs are ahead of id
// (0 = next or running).
func (s *Store) QueuePosition(ctx context.Context, id int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE id < ? AND status IN `+inList(len(ActiveStatuses)),
		append([]any{id}, strArgs(ActiveStatuses)...)...).Scan(&n)
	return n, err
}

// CountActive returns the number of active jobs.
func (s *Store) CountActive(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status IN `+inList(len(ActiveStatuses)), strArgs(ActiveStatuses)...).Scan(&n)
	return n, err
}

// Transition is a status change applied by SetStatus.
type Transition struct {
	Status        string
	FailureReason string
	// Stamp names the timestamp column to set to now (started_at, vpn_checked_at,
	// search_done_at, analyzed_at). finished_at is set automatically for final statuses.
	Stamp string
}

var stampCols = map[string]bool{"started_at": true, "vpn_checked_at": true, "search_done_at": true, "analyzed_at": true}

// ErrConflict is returned when a conditional update finds the row in another state.
var ErrConflict = errors.New("state changed concurrently")

// SetStatus moves job id to t.Status, but only if its current status is one of
// from (empty = any). It returns ErrConflict otherwise.
func (s *Store) SetStatus(ctx context.Context, id int64, from []string, t Transition) error {
	now := ts(s.now())
	sets := []string{"status = ?", "failure_reason = ?"}
	args := []any{t.Status, sql.NullString{String: t.FailureReason, Valid: t.FailureReason != ""}}
	if t.Stamp != "" {
		if !stampCols[t.Stamp] {
			return errors.New("invalid stamp column " + t.Stamp)
		}
		sets, args = append(sets, t.Stamp+" = ?"), append(args, now)
	}
	if IsFinal(t.Status) {
		sets, args = append(sets, "finished_at = ?"), append(args, now)
	}
	switch t.Status {
	case StatusWaitingVPN, StatusWaitingSplunk:
		sets = append(sets, "waiting_since = COALESCE(waiting_since, ?)")
		args = append(args, now)
	case StatusQueued, StatusCheckingVPN:
	default:
		sets = append(sets, "waiting_since = NULL")
	}
	q := `UPDATE jobs SET ` + strings.Join(sets, ", ") + ` WHERE id = ?`
	args = append(args, id)
	if len(from) > 0 {
		q += ` AND status IN ` + inList(len(from))
		args = append(args, strArgs(from)...)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetJob(ctx, id); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

// FailRunning marks every running job FAILED (used at startup after a crash
// or restart). It returns the affected jobs.
func (s *Store) FailRunning(ctx context.Context, reason string) ([]Job, error) {
	return s.bulkFinish(ctx, RunningStatuses, nil, StatusFailed, reason)
}

// ExpireWaiting marks WAITING_VPN/WAITING_SPLUNK jobs that have waited since
// before cutoff as EXPIRED.
func (s *Store) ExpireWaiting(ctx context.Context, cutoff time.Time, reason string) ([]Job, error) {
	return s.bulkFinish(ctx, []string{StatusWaitingVPN, StatusWaitingSplunk}, &cutoff, StatusExpired, reason)
}

func (s *Store) bulkFinish(ctx context.Context, from []string, waitingBefore *time.Time, to, reason string) ([]Job, error) {
	q := `SELECT ` + jobCols + jobFrom + ` WHERE j.status IN ` + inList(len(from))
	args := strArgs(from)
	if waitingBefore != nil {
		q += ` AND j.waiting_since < ?`
		args = append(args, ts(*waitingBefore))
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var jobs []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	var done []Job
	for _, j := range jobs {
		err := s.SetStatus(ctx, j.ID, from, Transition{Status: to, FailureReason: reason})
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return done, err
		}
		j.Status, j.FailureReason = to, reason
		done = append(done, j)
	}
	return done, nil
}

// Investigation is the diagnosis stored for a finished job.
type Investigation struct {
	JobID           int64    `json:"jobId"`
	ErrorType       string   `json:"errorType,omitempty"`
	FailedComponent string   `json:"failedComponent,omitempty"`
	Severity        string   `json:"severity,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	LikelyCause     string   `json:"likelyCause,omitempty"`
	SuggestedAction string   `json:"suggestedAction,omitempty"`
	RelevantLogs    []string `json:"relevantLogs"`
	ErrorSource     string   `json:"errorSource,omitempty"`
	RawLogSnippet   string   `json:"rawLogSnippet,omitempty"`
	LLMFailed       bool     `json:"llmFailed"`
}

// SaveInvestigation inserts or replaces the investigation for a job.
func (s *Store) SaveInvestigation(ctx context.Context, inv Investigation) error {
	logs, _ := json.Marshal(nonNil(inv.RelevantLogs))
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO investigations
		(job_id, error_type, failed_component, severity, summary, likely_cause, suggested_action, relevant_logs, error_source, raw_log_snippet, llm_failed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.JobID, inv.ErrorType, inv.FailedComponent, inv.Severity, inv.Summary, inv.LikelyCause, inv.SuggestedAction,
		string(logs), inv.ErrorSource, inv.RawLogSnippet, inv.LLMFailed, ts(s.now()))
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// GetInvestigation loads the investigation for a job.
func (s *Store) GetInvestigation(ctx context.Context, jobID int64) (Investigation, error) {
	var inv Investigation
	var et, fc, sev, sum, lc, sa, logs, src, raw sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT job_id, error_type, failed_component, severity, summary, likely_cause, suggested_action,
		relevant_logs, error_source, raw_log_snippet, llm_failed FROM investigations WHERE job_id = ?`, jobID).
		Scan(&inv.JobID, &et, &fc, &sev, &sum, &lc, &sa, &logs, &src, &raw, &inv.LLMFailed)
	if errors.Is(err, sql.ErrNoRows) {
		return inv, ErrNotFound
	}
	if err != nil {
		return inv, err
	}
	inv.ErrorType, inv.FailedComponent, inv.Severity, inv.Summary = et.String, fc.String, sev.String, sum.String
	inv.LikelyCause, inv.SuggestedAction, inv.ErrorSource, inv.RawLogSnippet = lc.String, sa.String, src.String, raw.String
	if logs.Valid {
		_ = json.Unmarshal([]byte(logs.String), &inv.RelevantLogs)
	}
	inv.RelevantLogs = nonNil(inv.RelevantLogs)
	return inv, nil
}

// PurgeRawLogs clears raw log snippets of investigations created before cutoff.
func (s *Store) PurgeRawLogs(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE investigations SET raw_log_snippet = NULL, relevant_logs = '[]'
		WHERE created_at < ? AND (raw_log_snippet IS NOT NULL OR relevant_logs != '[]')`, ts(cutoff))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeDiagnoses deletes investigations created before cutoff; job metadata stays.
func (s *Store) PurgeDiagnoses(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM investigations WHERE created_at < ?`, ts(cutoff))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
