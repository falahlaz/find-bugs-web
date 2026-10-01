package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTelegramCooldown(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.URL.Path != "/botTOKEN/sendMessage" || r.Form.Get("chat_id") != "-100" || r.Form.Get("text") == "" {
			t.Errorf("bad request %s %v", r.URL.Path, r.Form)
		}
		calls.Add(1)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	n := NewTelegram("TOKEN", "-100").(*Telegram)
	n.BaseURL = srv.URL
	ctx := context.Background()
	n.Notify(ctx, "vpn", "down")
	n.Notify(ctx, "vpn", "down again")
	n.Reset("vpn")
	n.Notify(ctx, "vpn", "down third")
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
	if _, ok := NewTelegram("", "x").(Nop); !ok {
		t.Error("empty token should give Nop")
	}
}
