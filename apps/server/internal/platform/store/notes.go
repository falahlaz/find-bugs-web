package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Engineer note states and AI verdicts.
const (
	NoteActive   = "active"
	NoteArchived = "archived"

	NoteVerdictOK         = "ok"
	NoteVerdictOverridden = "overridden"
	NoteVerdictUnchecked  = "unchecked"
)

// AnyErrorType is the error type key of a note that covers every error of
// its component.
const AnyErrorType = "*"

// ValidNoteVerdict reports whether v is a known AI verdict.
func ValidNoteVerdict(v string) bool {
	return v == NoteVerdictOK || v == NoteVerdictOverridden || v == NoteVerdictUnchecked
}

// EngineerNote is an engineer's cause and QA recommendation for an error,
// matched to jobs by ComponentKey and ErrorTypeKey.
type EngineerNote struct {
	ID               int64     `json:"id"`
	ComponentKey     string    `json:"componentKey"`
	ErrorTypeKey     string    `json:"errorTypeKey"`
	Component        string    `json:"component"`
	ErrorType        string    `json:"errorType"`
	ErrorSource      string    `json:"errorSource,omitempty"`
	Cause            string    `json:"cause"`
	QARecommendation string    `json:"qaRecommendation"`
	CauseText        string    `json:"causeText"`
	QAText           string    `json:"qaText"`
	AIVerdict        string    `json:"aiVerdict" enum:"ok,overridden,unchecked"`
	SourceJobID      *int64    `json:"sourceJobId,omitempty"`
	State            string    `json:"state" enum:"active,archived"`
	CreatedBy        string    `json:"createdBy,omitempty"`
	UpdatedBy        string    `json:"updatedBy,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// NoteVersion is one saved revision of a note.
type NoteVersion struct {
	ID               int64     `json:"id"`
	Cause            string    `json:"cause"`
	QARecommendation string    `json:"qaRecommendation"`
	CauseText        string    `json:"causeText"`
	QAText           string    `json:"qaText"`
	AIVerdict        string    `json:"aiVerdict"`
	Username         string    `json:"username,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
}

// NoteInput saves a note. With NoteID set it updates that active note's
// texts; otherwise it updates the active note with the same keys, or
// creates one.
type NoteInput struct {
	NoteID                                     int64
	ComponentKey, ErrorTypeKey                 string
	Component, ErrorType, ErrorSource          string
	Cause, QARecommendation, CauseText, QAText string
	AIVerdict                                  string
	SourceJobID, UserID                        int64
}

const noteCols = `n.id, n.component_key, n.error_type_key, n.component, n.error_type, n.error_source, n.cause, n.qa_recommendation,
	n.cause_text, n.qa_text, n.ai_verdict, n.source_job_id, n.state, COALESCE(c.username, ''), COALESCE(u.username, ''), n.created_at, n.updated_at`

const noteFrom = ` FROM engineer_notes n LEFT JOIN users c ON c.id = n.created_by LEFT JOIN users u ON u.id = n.updated_by`

func scanNote(row interface{ Scan(...any) error }) (EngineerNote, error) {
	var n EngineerNote
	var job sql.NullInt64
	var created, updated string
	err := row.Scan(&n.ID, &n.ComponentKey, &n.ErrorTypeKey, &n.Component, &n.ErrorType, &n.ErrorSource, &n.Cause, &n.QARecommendation,
		&n.CauseText, &n.QAText, &n.AIVerdict, &job, &n.State, &n.CreatedBy, &n.UpdatedBy, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	if job.Valid {
		n.SourceJobID = &job.Int64
	}
	n.CreatedAt, n.UpdatedAt = parseTS(created), parseTS(updated)
	return n, err
}

// SaveNote creates or updates a note (see NoteInput) and records the
// revision. created reports whether a new note was made.
func (s *Store) SaveNote(ctx context.Context, in NoteInput) (note EngineerNote, created bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return note, false, err
	}
	defer tx.Rollback()
	now := ts(s.now())
	uid := sql.NullInt64{Int64: in.UserID, Valid: in.UserID != 0}
	id := in.NoteID
	if id == 0 {
		err = tx.QueryRowContext(ctx, `SELECT id FROM engineer_notes WHERE component_key = ? AND error_type_key = ? AND state = ?`,
			in.ComponentKey, in.ErrorTypeKey, NoteActive).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return note, false, err
		}
	}
	if id == 0 {
		job := sql.NullInt64{Int64: in.SourceJobID, Valid: in.SourceJobID != 0}
		res, err := tx.ExecContext(ctx, `INSERT INTO engineer_notes (component_key, error_type_key, component, error_type, error_source,
			cause, qa_recommendation, cause_text, qa_text, ai_verdict, source_job_id, state, created_by, updated_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.ComponentKey, in.ErrorTypeKey, in.Component, in.ErrorType, in.ErrorSource,
			in.Cause, in.QARecommendation, in.CauseText, in.QAText, in.AIVerdict, job, NoteActive, uid, uid, now, now)
		if err != nil {
			return note, false, err
		}
		if id, err = res.LastInsertId(); err != nil {
			return note, false, err
		}
		created = true
	} else {
		res, err := tx.ExecContext(ctx, `UPDATE engineer_notes SET cause = ?, qa_recommendation = ?, cause_text = ?, qa_text = ?, ai_verdict = ?,
			updated_by = ?, updated_at = ? WHERE id = ? AND state = ?`,
			in.Cause, in.QARecommendation, in.CauseText, in.QAText, in.AIVerdict, uid, now, id, NoteActive)
		if err != nil {
			return note, false, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return note, false, ErrNotFound
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO engineer_note_versions (note_id, cause, qa_recommendation, cause_text, qa_text, ai_verdict, user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, in.Cause, in.QARecommendation, in.CauseText, in.QAText, in.AIVerdict, uid, now); err != nil {
		return note, false, err
	}
	if err := tx.Commit(); err != nil {
		return note, false, err
	}
	note, err = s.GetNote(ctx, id)
	return note, created, err
}

// GetNote returns a note by ID.
func (s *Store) GetNote(ctx context.Context, id int64) (EngineerNote, error) {
	return scanNote(s.db.QueryRowContext(ctx, `SELECT `+noteCols+noteFrom+` WHERE n.id = ?`, id))
}

// FindNote returns the active note for a component and error type, falling
// back to the component's note for every error type.
func (s *Store) FindNote(ctx context.Context, componentKey, errorTypeKey string) (EngineerNote, error) {
	return scanNote(s.db.QueryRowContext(ctx, `SELECT `+noteCols+noteFrom+`
		WHERE n.component_key = ? AND n.error_type_key IN (?, ?) AND n.state = ?
		ORDER BY n.error_type_key = ? LIMIT 1`, componentKey, errorTypeKey, AnyErrorType, NoteActive, AnyErrorType))
}

// ListNotes returns the notes in state ("" for all), most recently updated first.
func (s *Store) ListNotes(ctx context.Context, state string) ([]EngineerNote, error) {
	q, args := `SELECT `+noteCols+noteFrom, []any{}
	if state != "" {
		q, args = q+` WHERE n.state = ?`, append(args, state)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY n.updated_at DESC, n.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EngineerNote{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ArchiveNote retires an active note so it no longer matches jobs.
func (s *Store) ArchiveNote(ctx context.Context, id, userID int64) error {
	uid := sql.NullInt64{Int64: userID, Valid: userID != 0}
	res, err := s.db.ExecContext(ctx, `UPDATE engineer_notes SET state = ?, updated_by = ?, updated_at = ? WHERE id = ? AND state = ?`,
		NoteArchived, uid, ts(s.now()), id, NoteActive)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListNoteVersions returns a note's revisions, newest first.
func (s *Store) ListNoteVersions(ctx context.Context, noteID int64) ([]NoteVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT v.id, v.cause, v.qa_recommendation, v.cause_text, v.qa_text, v.ai_verdict, COALESCE(u.username, ''), v.created_at
		FROM engineer_note_versions v LEFT JOIN users u ON u.id = v.user_id WHERE v.note_id = ? ORDER BY v.id DESC`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NoteVersion{}
	for rows.Next() {
		var v NoteVersion
		var at string
		if err := rows.Scan(&v.ID, &v.Cause, &v.QARecommendation, &v.CauseText, &v.QAText, &v.AIVerdict, &v.Username, &at); err != nil {
			return nil, err
		}
		v.CreatedAt = parseTS(at)
		out = append(out, v)
	}
	return out, rows.Err()
}
