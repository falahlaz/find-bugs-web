package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ErrTraceBusy means a trace session is already answering.
var ErrTraceBusy = errors.New("trace session busy")

// Trace session states. A session that is open but idle for longer than
// the configured time is shown as closed without being stored as such.
const (
	TraceOpen   = "open"
	TraceClosed = "closed"
	// TracePurged sessions lost their Claude Code files; the history stays
	// readable and reopening starts a new conversation with a recap.
	TracePurged = "purged"
)

// Trace message roles, kinds and statuses.
const (
	TraceRoleUser      = "user"
	TraceRoleAssistant = "assistant"
	TraceKindChat      = "chat"
	TraceKindRetrace   = "retrace"
	TraceMsgQueued     = "queued"
	TraceMsgRunning    = "running"
	TraceMsgDone       = "done"
	TraceMsgFailed     = "failed"
)

// TraceRepo is a checkout a trace session may read.
type TraceRepo struct {
	Container string `json:"container"`
	Project   string `json:"project"`
	Dir       string `json:"dir"`
	Commit    string `json:"commit"`
	Ref       string `json:"ref"`
	RefSource string `json:"refSource"`
	Env       string `json:"env,omitempty"`
}

// TraceSession is the Claude Code conversation of a job's code trace.
type TraceSession struct {
	JobID     int64
	SessionID string
	Dir       string
	Repos     []TraceRepo
	State     string
	// Restart means the next turn starts a new conversation (with a recap)
	// because the old one was purged.
	Restart        bool
	Busy           bool
	LastActivityAt time.Time
	CreatedAt      time.Time
}

// TraceMessage is a question, answer or re-trace in a trace session.
type TraceMessage struct {
	ID         int64      `json:"id"`
	Role       string     `json:"role"`
	Kind       string     `json:"kind"`
	Username   string     `json:"username,omitempty"`
	Content    string     `json:"content"`
	CodeTrace  *CodeTrace `json:"codeTrace,omitempty"`
	Progress   []string   `json:"progress"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	Model      string     `json:"model,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// SaveTraceSession inserts or replaces a job's trace session.
func (s *Store) SaveTraceSession(ctx context.Context, sess TraceSession) error {
	repos, _ := json.Marshal(sess.Repos)
	now := s.now()
	if sess.State == "" {
		sess.State = TraceOpen
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO trace_sessions
		(job_id, session_id, dir, repos, state, restart, busy, last_activity_at, created_at) VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		sess.JobID, sess.SessionID, sess.Dir, string(repos), sess.State, sess.Restart, ts(now), ts(now))
	return err
}

const traceSessionCols = `job_id, session_id, dir, repos, state, restart, busy, last_activity_at, created_at`

func scanTraceSession(row interface{ Scan(...any) error }) (TraceSession, error) {
	var t TraceSession
	var repos, last, created string
	if err := row.Scan(&t.JobID, &t.SessionID, &t.Dir, &repos, &t.State, &t.Restart, &t.Busy, &last, &created); err != nil {
		return t, err
	}
	_ = json.Unmarshal([]byte(repos), &t.Repos)
	t.LastActivityAt, t.CreatedAt = parseTS(last), parseTS(created)
	return t, nil
}

// GetTraceSession loads a job's trace session.
func (s *Store) GetTraceSession(ctx context.Context, jobID int64) (TraceSession, error) {
	t, err := scanTraceSession(s.db.QueryRowContext(ctx, `SELECT `+traceSessionCols+` FROM trace_sessions WHERE job_id = ?`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// StartTraceTurn marks the session busy and records the engineer's message
// and a queued answer, returning the answer's ID. It fails with
// ErrTraceBusy if another turn is still going.
func (s *Store) StartTraceTurn(ctx context.Context, jobID, userID int64, kind, text string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := ts(s.now())
	res, err := tx.ExecContext(ctx, `UPDATE trace_sessions SET busy = 1, state = CASE WHEN state = 'purged' THEN state ELSE 'open' END,
		last_activity_at = ? WHERE job_id = ? AND busy = 0`, now, jobID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM trace_sessions WHERE job_id = ?`, jobID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, ErrTraceBusy
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO trace_messages (job_id, role, kind, user_id, content, status, created_at, finished_at)
		VALUES (?, 'user', ?, ?, ?, 'done', ?, ?)`, jobID, kind, userID, text, now, now); err != nil {
		return 0, err
	}
	res, err = tx.ExecContext(ctx, `INSERT INTO trace_messages (job_id, role, kind, status, created_at) VALUES (?, 'assistant', ?, 'queued', ?)`, jobID, kind, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// maxTraceProgress caps the progress lines kept per answer.
const maxTraceProgress = 60

// SetTraceRunning marks a queued answer as running.
func (s *Store) SetTraceRunning(ctx context.Context, msgID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE trace_messages SET status = 'running' WHERE id = ? AND status = 'queued'`, msgID)
	return err
}

// AddTraceProgress appends a progress line to a running answer, keeping
// the last maxTraceProgress lines.
func (s *Store) AddTraceProgress(ctx context.Context, msgID int64, line string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT progress FROM trace_messages WHERE id = ?`, msgID).Scan(&raw); err != nil {
		return err
	}
	var lines []string
	_ = json.Unmarshal([]byte(raw), &lines)
	lines = append(lines, line)
	if len(lines) > maxTraceProgress {
		lines = lines[len(lines)-maxTraceProgress:]
	}
	b, _ := json.Marshal(lines)
	if _, err := tx.ExecContext(ctx, `UPDATE trace_messages SET progress = ? WHERE id = ?`, string(b), msgID); err != nil {
		return err
	}
	return tx.Commit()
}

// TraceTurnResult is how a turn ended.
type TraceTurnResult struct {
	Content   string
	CodeTrace *CodeTrace
	Model     string
	Err       string // set when the turn failed
	// Repos, when set, replaces the session's checkouts (a re-trace adds one).
	Repos []TraceRepo
	// Started is set when the turn began a new conversation, which later
	// turns resume.
	Started bool
}

// FinishTraceTurn stores the answer and frees the session.
func (s *Store) FinishTraceTurn(ctx context.Context, jobID, msgID int64, r TraceTurnResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := ts(s.now())
	status, errMsg := TraceMsgDone, sql.NullString{}
	if r.Err != "" {
		status, errMsg = TraceMsgFailed, sql.NullString{String: r.Err, Valid: true}
	}
	var trace sql.NullString
	if r.CodeTrace != nil {
		b, _ := json.Marshal(r.CodeTrace)
		trace = sql.NullString{String: string(b), Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE trace_messages SET status = ?, content = ?, code_trace = ?, model = ?, error = ?, finished_at = ? WHERE id = ?`,
		status, r.Content, trace, r.Model, errMsg, now, msgID); err != nil {
		return err
	}
	q := `UPDATE trace_sessions SET busy = 0, last_activity_at = ?`
	args := []any{now}
	if r.Repos != nil {
		b, _ := json.Marshal(r.Repos)
		q += `, repos = ?`
		args = append(args, string(b))
	}
	if r.Started {
		q += `, restart = 0, state = 'open'`
	}
	if _, err := tx.ExecContext(ctx, q+` WHERE job_id = ?`, append(args, jobID)...); err != nil {
		return err
	}
	return tx.Commit()
}

// SetTraceState closes or reopens a session. Reopening counts as activity,
// so an idle session does not close again straight away. Reopening a
// purged session gives it a new conversation ID that the next turn starts.
func (s *Store) SetTraceState(ctx context.Context, jobID int64, state, newSessionID string) error {
	now := ts(s.now())
	var res sql.Result
	var err error
	switch state {
	case TraceOpen:
		res, err = s.db.ExecContext(ctx, `UPDATE trace_sessions SET
			restart = CASE WHEN state = 'purged' THEN 1 ELSE restart END,
			session_id = CASE WHEN state = 'purged' THEN ? ELSE session_id END,
			state = 'open', last_activity_at = ? WHERE job_id = ?`, newSessionID, now, jobID)
	case TraceClosed:
		res, err = s.db.ExecContext(ctx, `UPDATE trace_sessions SET state = 'closed' WHERE job_id = ? AND state != 'purged'`, jobID)
	case TracePurged:
		res, err = s.db.ExecContext(ctx, `UPDATE trace_sessions SET state = 'purged' WHERE job_id = ? AND busy = 0`, jobID)
	default:
		return errors.New("invalid trace session state")
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetTraceSession(ctx, jobID); err != nil {
			return err
		}
	}
	return nil
}

// ListTraceMessages returns a session's messages, oldest first.
func (s *Store) ListTraceMessages(ctx context.Context, jobID int64) ([]TraceMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.role, m.kind, COALESCE(u.username, ''), m.content, m.code_trace, m.progress,
		m.status, COALESCE(m.error, ''), COALESCE(m.model, ''), m.created_at, m.finished_at
		FROM trace_messages m LEFT JOIN users u ON u.id = m.user_id WHERE m.job_id = ? ORDER BY m.id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TraceMessage{}
	for rows.Next() {
		var m TraceMessage
		var trace, finished sql.NullString
		var progress, created string
		if err := rows.Scan(&m.ID, &m.Role, &m.Kind, &m.Username, &m.Content, &trace, &progress, &m.Status, &m.Error, &m.Model, &created, &finished); err != nil {
			return nil, err
		}
		if trace.Valid {
			var t CodeTrace
			if json.Unmarshal([]byte(trace.String), &t) == nil {
				m.CodeTrace = &t
			}
		}
		_ = json.Unmarshal([]byte(progress), &m.Progress)
		if m.Progress == nil {
			m.Progress = []string{}
		}
		m.CreatedAt, m.FinishedAt = parseTS(created), parseNullTS(finished)
		out = append(out, m)
	}
	return out, rows.Err()
}

// RecoverTraceTurns fails answers left queued or running by a previous
// process and frees their sessions.
func (s *Store) RecoverTraceTurns(ctx context.Context, reason string) error {
	now := ts(s.now())
	if _, err := s.db.ExecContext(ctx, `UPDATE trace_messages SET status = 'failed', error = ?, finished_at = ? WHERE status IN ('queued', 'running')`, reason, now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE trace_sessions SET busy = 0 WHERE busy = 1`)
	return err
}

// StaleTraceSessions lists sessions not yet purged and idle since before
// cutoff.
func (s *Store) StaleTraceSessions(ctx context.Context, cutoff time.Time) ([]TraceSession, error) {
	return s.queryTraceSessions(ctx, `WHERE state != 'purged' AND busy = 0 AND last_activity_at < ?`, ts(cutoff))
}

// ActiveTraceDirs lists the checkout directories of sessions not purged.
func (s *Store) ActiveTraceDirs(ctx context.Context) (map[string]bool, error) {
	sessions, err := s.queryTraceSessions(ctx, `WHERE state != 'purged'`)
	if err != nil {
		return nil, err
	}
	dirs := map[string]bool{}
	for _, t := range sessions {
		for _, r := range t.Repos {
			dirs[r.Dir] = true
		}
	}
	return dirs, nil
}

func (s *Store) queryTraceSessions(ctx context.Context, where string, args ...any) ([]TraceSession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+traceSessionCols+` FROM trace_sessions `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TraceSession
	for rows.Next() {
		t, err := scanTraceSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
