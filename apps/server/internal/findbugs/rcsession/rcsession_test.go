package rcsession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (*Manager, string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "grp", "svc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, ".fake-log")
	t.Setenv("GITLAB_TOKEN", "glpat-secret")
	script, _ := filepath.Abs("testdata/fake-rc-session.sh")
	tmux, _ := filepath.Abs("testdata/fake-tmux.sh")
	m := New(Config{Script: script, ClaudeBin: "/opt/claude/bin/claude", Root: root, Tmux: tmux})
	return m, dir, log
}

func TestStartStop(t *testing.T) {
	m, dir, log := setup(t)
	if !m.Enabled() || New(Config{Script: "/nope"}).Enabled() {
		t.Fatal("Enabled wrong")
	}
	ctx := context.Background()
	out, err := m.Start(ctx, dir+"/")
	if err != nil || !strings.Contains(out, "environment=env_1") {
		t.Fatalf("Start = %q, %v", out, err)
	}
	got, _ := os.ReadFile(log)
	if !strings.Contains(string(got), "args: start "+dir+"\n") {
		t.Fatalf("script args:\n%s", got)
	}
	// The service's secrets stay out of tmux; claude is found on PATH.
	if strings.Contains(string(got), "glpat-secret") || !strings.Contains(string(got), "RC_SESSION_ROOT=") ||
		!strings.Contains(string(got), "PATH=/opt/claude/bin:") {
		t.Fatalf("script env:\n%s", got)
	}

	if out, err := m.Stop(ctx, dir); err != nil || !strings.Contains(out, "Killed") {
		t.Fatalf("Stop = %q, %v", out, err)
	}
	os.Mkdir(filepath.Join(dir, "fail"), 0o755)
	if _, err := m.Stop(ctx, dir); err == nil || strings.Contains(err.Error(), "abc") || !strings.Contains(err.Error(), "leftover") {
		t.Fatalf("failed Stop err = %v", err)
	}

	for _, bad := range []string{"grp/svc", filepath.Dir(filepath.Dir(dir)), filepath.Join(dir, "..", "..", ".."), "/etc"} {
		if _, err := m.Start(ctx, bad); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("Start(%q) err = %v", bad, err)
		}
	}
}

func TestSessions(t *testing.T) {
	m, dir, _ := setup(t)
	ctx := context.Background()
	if s, err := m.Sessions(ctx); err != nil || len(s) != 0 {
		t.Fatalf("no server: %v, %v", s, err)
	}
	os.WriteFile(filepath.Join(m.cfg.Root, ".fake-tmux-ls"), []byte("svc|"+dir+"|1759651200\nmain||1759651200\n"), 0o644)
	s, err := m.Sessions(ctx)
	if err != nil || len(s) != 1 {
		t.Fatalf("Sessions = %v, %v", s, err)
	}
	if got := s[dir]; got.Name != "svc" || got.URL != "https://claude.ai/code?environment=env_svc" || got.Since.Unix() != 1759651200 {
		t.Fatalf("session = %+v", got)
	}
	// Another folder with the same name cannot take over the session.
	other := filepath.Join(m.cfg.Root, "other", "svc")
	os.MkdirAll(other, 0o755)
	if _, err := m.Start(ctx, other); err == nil || !strings.Contains(err.Error(), "folder lain") {
		t.Fatalf("Start clash err = %v", err)
	}
	if Name("/x/my.svc") != "my-svc" {
		t.Fatal("Name")
	}
}
