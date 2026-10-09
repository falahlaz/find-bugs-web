package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const agySample = `{"models":{
"gemini-3.6-flash-high":{"displayName":"Gemini 3.6 Flash (High)","modelProvider":"MODEL_PROVIDER_GOOGLE","quotaInfo":{"remainingFraction":0.75,"resetTime":"2026-10-09T08:05:51Z"}},
"gemini-3.1-pro-low":{"displayName":"Gemini 3.1 Pro (Low)","modelProvider":"MODEL_PROVIDER_GOOGLE","quotaInfo":{"remainingFraction":0.75,"resetTime":"2026-10-09T08:05:51Z"}},
"gemini-3-flash":{"modelProvider":"MODEL_PROVIDER_GOOGLE","quotaInfo":{"remainingFraction":0.1,"resetTime":"2026-10-09T08:05:51Z"}},
"claude-opus-4-6-thinking":{"displayName":"Claude Opus 4.6 (Thinking)","modelProvider":"MODEL_PROVIDER_ANTHROPIC","quotaInfo":{"remainingFraction":1,"resetTime":"2026-10-09T08:41:35Z"}},
"gpt-oss-120b-medium":{"displayName":"GPT-OSS 120B (Medium)","modelProvider":"MODEL_PROVIDER_OPENAI","quotaInfo":{"resetTime":"2026-10-09T08:41:35Z"}},
"tab_flash_lite_preview":{"quotaInfo":{"remainingFraction":1}}
}}`

func TestParseAgy(t *testing.T) {
	s, err := ParseAgy([]byte(agySample))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pools) != 2 {
		t.Fatalf("pools %+v", s.Pools)
	}
	g, o := s.Pools[0], s.Pools[1]
	if g.Name != "Gemini" || g.Percent != 25 || len(g.Models) != 2 || g.ResetsAt.Format(time.RFC3339) != "2026-10-09T08:05:51Z" {
		t.Fatalf("gemini pool %+v (unnamed models must not count)", g)
	}
	if o.Name != "Claude & GPT-OSS" || o.Percent != 0 || len(o.Models) != 2 {
		t.Fatalf("other pool %+v", o)
	}
	if _, err := ParseAgy([]byte(`{"models":{}}`)); err == nil {
		t.Fatal("want error without models")
	}
}

func writeToken(t *testing.T, dir, access string, expiry time.Time) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"token": map[string]any{"access_token": access, "expiry": expiry}})
	if err := os.WriteFile(filepath.Join(dir, "antigravity-oauth-token"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAgyGetRefreshesExpiredToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" || r.Header.Get("User-Agent") != "antigravity" {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		w.Write([]byte(agySample))
	}))
	defer srv.Close()
	dir := t.TempDir()
	writeToken(t, dir, "stale", time.Now().Add(-time.Hour))
	refreshes := 0
	p := &AgyProber{StateDir: dir, Endpoint: srv.URL, Timeout: 5 * time.Second, MaxAge: time.Hour, refresh: func(context.Context) error {
		refreshes++
		writeToken(t, dir, "fresh", time.Now().Add(time.Hour))
		return nil
	}}
	s, err := p.Get(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if refreshes != 1 || len(s.Pools) != 2 || s.CheckedAt.IsZero() {
		t.Fatalf("refreshes=%d snapshot=%+v", refreshes, s)
	}
	if _, err := p.Get(context.Background(), false); err != nil || refreshes != 1 {
		t.Fatalf("second get should be cached: refreshes=%d err=%v", refreshes, err)
	}
}
