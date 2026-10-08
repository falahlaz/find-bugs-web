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
	agy := filepath.Join(root, ".bin", "agy")
	os.MkdirAll(filepath.Dir(agy), 0o755)
	os.WriteFile(agy, []byte("#!/bin/sh\n"), 0o755)
	m := New(Config{Script: script, ClaudeBin: "/opt/claude/bin/claude", AgyBin: agy, Root: root, Tmux: tmux})
	return m, dir, log
}

func TestStartStop(t *testing.T) {
	m, dir, log := setup(t)
	if !m.Enabled() || New(Config{Script: "/nope"}).Enabled() {
		t.Fatal("Enabled wrong")
	}
	ctx := context.Background()
	out, err := m.Start(ctx, dir+"/", Claude)
	if err != nil || !strings.Contains(out, "environment=env_1") {
		t.Fatalf("Start = %q, %v", out, err)
	}
	got, _ := os.ReadFile(log)
	if !strings.Contains(string(got), "args: start "+dir+"\n") {
		t.Fatalf("script args:\n%s", got)
	}
	// The service's secrets stay out of tmux; claude is found on PATH.
	if strings.Contains(string(got), "glpat-secret") || !strings.Contains(string(got), "RC_SESSION_ROOT=") ||
		!strings.Contains(string(got), "/opt/claude/bin:/usr/local/bin") {
		t.Fatalf("script env:\n%s", got)
	}

	// agy sessions are started with --agy, agy found on PATH too.
	if _, err := m.Start(ctx, dir, Agy); err != nil {
		t.Fatalf("Start agy: %v", err)
	}
	got, _ = os.ReadFile(log)
	if !strings.Contains(string(got), "args: start "+dir+" --agy\n") || !strings.Contains(string(got), "PATH="+filepath.Join(m.cfg.Root, ".bin")+":/opt/claude/bin:") {
		t.Fatalf("agy script call:\n%s", got)
	}
	if _, err := m.Start(ctx, dir, "codex"); err == nil {
		t.Error("an unknown engine should fail")
	}
	noAgy := New(Config{Script: m.cfg.Script, AgyBin: "/nope/agy", Root: m.cfg.Root, Tmux: m.cfg.Tmux})
	if noAgy.AgyEnabled() || !m.AgyEnabled() {
		t.Fatal("AgyEnabled wrong")
	}
	if _, err := noAgy.Start(ctx, dir, Agy); err == nil || !strings.Contains(err.Error(), "agy") {
		t.Errorf("Start agy without agy = %v", err)
	}

	if out, err := m.Stop(ctx, dir); err != nil || !strings.Contains(out, "Killed") {
		t.Fatalf("Stop = %q, %v", out, err)
	}
	os.Mkdir(filepath.Join(dir, "fail"), 0o755)
	if _, err := m.Stop(ctx, dir); err == nil || strings.Contains(err.Error(), "abc") || !strings.Contains(err.Error(), "leftover") {
		t.Fatalf("failed Stop err = %v", err)
	}

	for _, bad := range []string{"grp/svc", filepath.Dir(filepath.Dir(dir)), filepath.Join(dir, "..", "..", ".."), "/etc"} {
		if _, err := m.Start(ctx, bad, Claude); !errors.Is(err, ErrOutsideRoot) {
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
	gw := filepath.Join(m.cfg.Root, "grp", "gw")
	web := filepath.Join(m.cfg.Root, "grp", "web")
	os.WriteFile(filepath.Join(m.cfg.Root, ".fake-tmux-ls"), []byte("svc|"+dir+"|1759651200|\nmain||1759651200|\ngw|"+gw+"|1759651200|agy\nweb|"+web+"|1759651200|agy|https://antigravity.google.com/r/inst_web?p=c%2Fconv-9\n"), 0o644)
	s, err := m.Sessions(ctx)
	if err != nil || len(s) != 3 {
		t.Fatalf("Sessions = %v, %v", s, err)
	}
	if got := s[dir]; got.Name != "svc" || got.Engine != Claude || got.URL != "https://claude.ai/code?environment=env_svc" || got.Since.Unix() != 1759651200 {
		t.Fatalf("session = %+v", got)
	}
	if got := s[gw]; got.Engine != Agy || got.URL != "https://antigravity.google.com/r/inst_gw?p=c%2Fconv-1" {
		t.Fatalf("agy session = %+v", got)
	}
	if got := s[web]; got.Engine != Agy || got.URL != "https://antigravity.google.com/r/inst_web?p=c%2Fconv-9" {
		t.Fatalf("agy session with stored link = %+v", got)
	}
	// Another folder with the same name cannot take over the session.
	other := filepath.Join(m.cfg.Root, "other", "svc")
	os.MkdirAll(other, 0o755)
	if _, err := m.Start(ctx, other, Claude); err == nil || !strings.Contains(err.Error(), "folder lain") {
		t.Fatalf("Start clash err = %v", err)
	}
	if Name("/x/my.svc") != "my-svc" {
		t.Fatal("Name")
	}
}
