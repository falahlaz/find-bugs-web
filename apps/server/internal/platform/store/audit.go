package store

import (
	"context"
	"database/sql"
	"time"
)

// AuditEntry records who did what to the shared VPN/Splunk sessions.
type AuditEntry struct {
	ID       int64     `json:"id"`
	UserID   *int64    `json:"userId,omitempty"`
	Username string    `json:"username,omitempty"`
	Action   string    `json:"action"`
	Result   string    `json:"result"`
	Detail   string    `json:"detail,omitempty"`
	At       time.Time `json:"at"`
}

// AddAudit inserts an audit entry. userID 0 means the system.
func (s *Store) AddAudit(ctx context.Context, userID int64, action, result, detail string) error {
	uid := sql.NullInt64{Int64: userID, Valid: userID != 0}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit (user_id, action, result, detail, at) VALUES (?, ?, ?, ?, ?)`,
		uid, action, result, detail, ts(s.now()))
	return err
}

// ListAudit returns the newest entries first.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.user_id, COALESCE(u.username, ''), a.action, a.result, COALESCE(a.detail, ''), a.at
		FROM audit a LEFT JOIN users u ON u.id = a.user_id ORDER BY a.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var uid sql.NullInt64
		var at string
		if err := rows.Scan(&e.ID, &uid, &e.Username, &e.Action, &e.Result, &e.Detail, &at); err != nil {
			return nil, err
		}
		if uid.Valid {
			e.UserID = &uid.Int64
		}
		e.At = parseTS(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
