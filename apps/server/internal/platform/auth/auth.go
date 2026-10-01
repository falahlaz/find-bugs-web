// Package auth implements password login, cookie sessions, CSRF protection
// and role checks.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

// Cookie and header names.
const (
	CookieName = "fbw_session"
	CSRFHeader = "X-CSRF-Token"
	MinPassLen = 8
)

// ErrBadCredentials is returned for a wrong username or password.
var ErrBadCredentials = errors.New("username atau password salah")

// HashPassword returns a bcrypt hash.
func HashPassword(pw string) (string, error) {
	if len(pw) < MinPassLen {
		return "", errors.New("password minimal 8 karakter")
	}
	if len(pw) > 72 {
		return "", errors.New("password maksimal 72 karakter")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// dummyHash keeps timing similar when the user does not exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// Service manages sessions.
type Service struct {
	Store   *store.Store
	TTL     time.Duration
	Secure  bool
	limiter *limiter
}

// NewService returns a Service with a login rate limit of 10 attempts per
// 15 minutes per IP and per username.
func NewService(s *store.Store, ttl time.Duration, secure bool) *Service {
	return &Service{Store: s, TTL: ttl, Secure: secure, limiter: newLimiter(10, 15*time.Minute)}
}

// ErrRateLimited is returned when too many logins failed recently.
var ErrRateLimited = errors.New("terlalu banyak percobaan login, coba lagi nanti")

// Login checks credentials and creates a session. It returns the session
// token (for the cookie), the CSRF token and the user.
func (s *Service) Login(ctx context.Context, username, password, ip string) (string, string, store.User, error) {
	keys := []string{"ip:" + ip, "user:" + strings.ToLower(username)}
	if !s.limiter.allow(keys...) {
		return "", "", store.User{}, ErrRateLimited
	}
	u, err := s.Store.GetUserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		s.limiter.fail(keys...)
		return "", "", u, ErrBadCredentials
	}
	if err != nil {
		return "", "", u, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil || !u.Active {
		s.limiter.fail(keys...)
		return "", "", store.User{}, ErrBadCredentials
	}
	s.limiter.reset(keys...)
	token, csrf := randomToken(), randomToken()
	err = s.Store.CreateSession(ctx, store.Session{TokenHash: hashToken(token), UserID: u.ID, CSRFToken: csrf, ExpiresAt: time.Now().Add(s.TTL)})
	return token, csrf, u, err
}

// SetCookie writes the session cookie.
func (s *Service) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.Secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(s.TTL.Seconds())})
}

// ClearCookie removes the session cookie.
func (s *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: s.Secure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// Logout deletes the session behind the request cookie.
func (s *Service) Logout(ctx context.Context, r *http.Request) error {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return nil
	}
	return s.Store.DeleteSession(ctx, hashToken(c.Value))
}

type ctxKey struct{}

// Identity is the authenticated caller.
type Identity struct {
	User store.User
	CSRF string
}

// FromContext returns the caller set by Require.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// WithIdentity returns ctx carrying id (tests and internal callers).
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// Require authenticates the request and, for unsafe methods, checks the CSRF
// header. roles, if given, restricts access to those roles.
func (s *Service) Require(next http.Handler, roles ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil || c.Value == "" {
			httpx.Error(w, http.StatusUnauthorized, "unauthenticated", "Silakan login dulu.")
			return
		}
		sess, u, err := s.Store.GetSession(r.Context(), hashToken(c.Value))
		if errors.Is(err, store.ErrNotFound) {
			s.ClearCookie(w)
			httpx.Error(w, http.StatusUnauthorized, "unauthenticated", "Sesi berakhir, silakan login lagi.")
			return
		}
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			got := r.Header.Get(CSRFHeader)
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRFToken)) != 1 {
				httpx.Error(w, http.StatusForbidden, "csrf", "Token CSRF tidak valid. Muat ulang halaman.")
				return
			}
		}
		if len(roles) > 0 && !contains(roles, u.Role) {
			httpx.Error(w, http.StatusForbidden, "forbidden", "Role kamu tidak punya akses ke fitur ini.")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), Identity{User: u, CSRF: sess.CSRFToken})))
	})
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// ClientIP returns the caller IP, trusting CF-Connecting-IP only when the
// request comes from loopback (cloudflared runs on the same host).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
			return cf
		}
	}
	return host
}

// limiter counts failed attempts per key in a sliding window.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *limiter) prune(k string, now time.Time) []time.Time {
	h := l.hits[k]
	i := 0
	for i < len(h) && now.Sub(h[i]) > l.window {
		i++
	}
	h = h[i:]
	if len(h) == 0 {
		delete(l.hits, k)
	} else {
		l.hits[k] = h
	}
	return h
}

func (l *limiter) allow(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		if len(l.prune(k, now)) >= l.max {
			return false
		}
	}
	return true
}

func (l *limiter) fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		l.hits[k] = append(l.prune(k, now), now)
	}
}

func (l *limiter) reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.hits, k)
	}
}
