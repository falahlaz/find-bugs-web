package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// Roles.
const (
	RoleQA       = "qa"
	RoleEngineer = "engineer"
)

// ValidRole reports whether r is a known role.
func ValidRole(r string) bool { return r == RoleQA || r == RoleEngineer }

// Menu is a page a QA can be granted. Engineers always get every menu.
type Menu string

// Menus.
const (
	MenuInvestigasi Menu = "investigasi"
	MenuKoneksi     Menu = "koneksi"
	MenuLaporan     Menu = "laporan"
	MenuTools       Menu = "tools"
)

// AllMenus lists every menu in nav order.
var AllMenus = []Menu{MenuInvestigasi, MenuKoneksi, MenuLaporan, MenuTools}

// EnumValues lists the menus for the OpenAPI spec.
func (Menu) EnumValues() []string {
	out := make([]string, len(AllMenus))
	for i, m := range AllMenus {
		out[i] = string(m)
	}
	return out
}

// NormalizeMenus dedupes ms into nav order; ok is false on an unknown menu.
func NormalizeMenus(ms []Menu) ([]Menu, bool) {
	for _, m := range ms {
		if !slices.Contains(AllMenus, m) {
			return nil, false
		}
	}
	out := []Menu{}
	for _, m := range AllMenus {
		if slices.Contains(ms, m) {
			out = append(out, m)
		}
	}
	return out, true
}

// ErrDuplicate is returned when a unique value already exists.
var ErrDuplicate = errors.New("already exists")

// User is a website account.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role" enum:"qa,engineer"`
	Active       bool      `json:"active"`
	Menus        []Menu    `json:"menus" doc:"Menus this user can open; engineers always get all"`
	CreatedAt    time.Time `json:"createdAt"`
}

// HasMenu reports whether u may open m.
func (u User) HasMenu(m Menu) bool {
	return u.Role == RoleEngineer || slices.Contains(u.Menus, m)
}

const userCols = `id, username, password_hash, role, active, menus, created_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var created string
	var menus sql.NullString
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Active, &menus, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	u.CreatedAt = parseTS(created)
	u.Menus = slices.Clone(AllMenus)
	if u.Role != RoleEngineer && menus.Valid {
		var ms []Menu
		if json.Unmarshal([]byte(menus.String), &ms) == nil {
			// Drop menus that no longer exist.
			ms = slices.DeleteFunc(ms, func(m Menu) bool { return !slices.Contains(AllMenus, m) })
			u.Menus, _ = NormalizeMenus(ms)
		}
	}
	return u, err
}

// CreateUser inserts a user.
func (s *Store) CreateUser(ctx context.Context, username, hash, role string) (User, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, active, created_at) VALUES (?, ?, ?, 1, ?)`,
		username, hash, role, ts(s.now()))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrDuplicate
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetUser(ctx, id)
}

// GetUser loads a user by ID.
func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

// GetUserByUsername loads a user by username (case-insensitive).
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, username))
}

// ListUsers returns all users ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserUpdate holds optional changes for UpdateUser.
type UserUpdate struct {
	Role         *string
	Active       *bool
	PasswordHash *string
	// Menus replaces the QA's menus (normalize it first).
	Menus *[]Menu
}

// UpdateUser applies the non-nil fields.
func (s *Store) UpdateUser(ctx context.Context, id int64, u UserUpdate) (User, error) {
	var sets []string
	var args []any
	if u.Role != nil {
		sets, args = append(sets, "role = ?"), append(args, *u.Role)
	}
	if u.Active != nil {
		sets, args = append(sets, "active = ?"), append(args, *u.Active)
	}
	if u.PasswordHash != nil {
		sets, args = append(sets, "password_hash = ?"), append(args, *u.PasswordHash)
	}
	if u.Menus != nil {
		b, err := json.Marshal(*u.Menus)
		if err != nil {
			return User{}, err
		}
		sets, args = append(sets, "menus = ?"), append(args, string(b))
	}
	if len(sets) > 0 {
		res, err := s.db.ExecContext(ctx, `UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, id)...)
		if err != nil {
			return User{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return User{}, ErrNotFound
		}
	}
	return s.GetUser(ctx, id)
}

// Session is a logged-in browser session.
type Session struct {
	TokenHash string
	UserID    int64
	CSRFToken string
	ExpiresAt time.Time
}

// CreateSession stores a session.
func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		sess.TokenHash, sess.UserID, sess.CSRFToken, ts(s.now()), ts(sess.ExpiresAt))
	return err
}

// GetSession returns a non-expired session and its active user.
func (s *Store) GetSession(ctx context.Context, tokenHash string) (Session, User, error) {
	var sess Session
	var exp string
	err := s.db.QueryRowContext(ctx, `SELECT token_hash, user_id, csrf_token, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).
		Scan(&sess.TokenHash, &sess.UserID, &sess.CSRFToken, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return sess, User{}, ErrNotFound
	}
	if err != nil {
		return sess, User{}, err
	}
	sess.ExpiresAt = parseTS(exp)
	if !s.now().Before(sess.ExpiresAt) {
		return sess, User{}, ErrNotFound
	}
	u, err := s.GetUser(ctx, sess.UserID)
	if err != nil {
		return sess, u, err
	}
	if !u.Active {
		return sess, u, ErrNotFound
	}
	return sess, u, nil
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteUserSessions logs a user out everywhere.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// PurgeExpiredSessions deletes expired sessions.
func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, ts(s.now()))
	return err
}
