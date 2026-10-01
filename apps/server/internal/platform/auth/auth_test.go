package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

func setup(t *testing.T) (*Service, store.User) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := st.CreateUser(context.Background(), "qa1", h, store.RoleQA)
	return NewService(st, time.Hour, true), u
}

func TestLoginAndRequire(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	if _, _, _, err := s.Login(ctx, "qa1", "wrong-pass", "1.1.1.1"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("wrong password err = %v", err)
	}
	if _, _, _, err := s.Login(ctx, "nobody", "whatever1", "1.1.1.1"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("unknown user err = %v", err)
	}
	token, csrf, u, err := s.Login(ctx, "QA1", "correct-horse", "1.1.1.1")
	if err != nil || u.Username != "qa1" {
		t.Fatalf("login = %+v, %v", u, err)
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := FromContext(r.Context())
		w.Write([]byte(id.User.Username))
	})
	do := func(h http.Handler, method, cookie, csrfHdr string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/x", nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
		}
		if csrfHdr != "" {
			r.Header.Set(CSRFHeader, csrfHdr)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	h := s.Require(ok)
	if w := do(h, "GET", "", ""); w.Code != 401 {
		t.Errorf("no cookie: %d", w.Code)
	}
	if w := do(h, "GET", token, ""); w.Code != 200 || w.Body.String() != "qa1" {
		t.Errorf("GET: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", token, ""); w.Code != 403 {
		t.Errorf("POST without csrf: %d", w.Code)
	}
	if w := do(h, "POST", token, csrf); w.Code != 200 {
		t.Errorf("POST with csrf: %d", w.Code)
	}
	if w := do(s.Require(ok, store.RoleEngineer), "GET", token, ""); w.Code != 403 {
		t.Errorf("role check: %d", w.Code)
	}
}

func TestRateLimit(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		s.Login(ctx, "qa1", "wrong-pass", "2.2.2.2")
	}
	if _, _, _, err := s.Login(ctx, "qa1", "correct-horse", "3.3.3.3"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limit by username, got %v", err)
	}
}

func TestHashPasswordLength(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Error("short password accepted")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("CF-Connecting-IP", "9.9.9.9")
	if ClientIP(r) != "9.9.9.9" {
		t.Error("loopback should trust CF header")
	}
	r.RemoteAddr = "8.8.8.8:5555"
	if ClientIP(r) != "8.8.8.8" {
		t.Error("remote must not trust CF header")
	}
}
