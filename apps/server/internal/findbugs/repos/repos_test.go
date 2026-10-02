package repos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
)

func TestContainer(t *testing.T) {
	for src, want := range map[string]string{
		"/var/log/pods/tdw-dev_service-payment-migration-5448bb778c-6sbnz_d75ca60c-f4b6-472e-b11b-f8d2e5f450bd/service-payment-migration/0.log": "service-payment-migration",
		"/var/log/pods/ns_pod-1_uid/app/12.log": "app",
		"/var/log/pods/ns_pod_uid/../etc/0.log": "",
		"/var/log/containers/x.log":             "",
		"":                                      "",
	} {
		if got := Container(src); got != want {
			t.Errorf("Container(%q) = %q, want %q", src, got, want)
		}
	}
	evs := []splunk.Event{
		{Source: "/var/log/pods/n_p_u/b/0.log"}, {Source: "/var/log/pods/n_p_u/a/0.log"},
		{Source: "/var/log/pods/n_p_u/b/0.log"}, {Source: "other"}, {Source: "/var/log/pods/n_p_u/c/0.log"},
	}
	if got := strings.Join(Containers(evs), ","); got != "b,a,c" {
		t.Errorf("Containers = %s", got)
	}
}

func TestProject(t *testing.T) {
	m := New(Config{Group: "grp", RepoMap: map[string]string{"web": "other/web-frontend"}})
	if p := m.Project("svc"); p != "grp/svc" {
		t.Errorf("Project(svc) = %s", p)
	}
	if p := m.Project("web"); p != "other/web-frontend" {
		t.Errorf("Project(web) = %s", p)
	}
	for _, p := range []string{"grp/svc", "a/b/c"} {
		if !validProject(p) {
			t.Errorf("%s should be valid", p)
		}
	}
	for _, p := range []string{"svc", "grp/..", "../x", "/abs/x", "grp/.git", "grp/a b"} {
		if validProject(p) {
			t.Errorf("%s should be invalid", p)
		}
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestSync clones from a local "GitLab" (bare repos under a directory served
// over file://), then fetches a new commit and cleans the worktree.
func TestSync(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "gitlab")
	work := filepath.Join(root, "work")
	git(t, root, "init", "-q", "--bare", "-b", "main", filepath.Join(remote, "grp", "svc.git"))
	git(t, root, "clone", "-q", filepath.Join(remote, "grp", "svc.git"), work)
	os.WriteFile(filepath.Join(work, "a.js"), []byte("v1\n"), 0o644)
	git(t, work, "add", ".")
	git(t, work, "commit", "-qm", "one")
	git(t, work, "push", "-q", "origin", "HEAD:main")

	m := New(Config{URL: "file://" + remote, Token: "glpat-secret", Dir: filepath.Join(root, "repos"), Group: "grp"})
	ctx := context.Background()
	co, err := m.Sync(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if co.Project != "grp/svc" || co.Dir != filepath.Join(root, "repos", "grp", "svc") || co.Commit != git(t, work, "rev-parse", "HEAD") {
		t.Fatalf("checkout = %+v", co)
	}
	if b, _ := os.ReadFile(filepath.Join(co.Dir, "a.js")); string(b) != "v1\n" {
		t.Fatalf("a.js = %q", b)
	}
	cfg, _ := os.ReadFile(filepath.Join(co.Dir, ".git", "config"))
	if strings.Contains(string(cfg), "secret") || strings.Contains(string(cfg), "extraHeader") {
		t.Fatalf("credentials leaked into .git/config:\n%s", cfg)
	}

	os.WriteFile(filepath.Join(work, "a.js"), []byte("v2\n"), 0o644)
	git(t, work, "commit", "-qam", "two")
	git(t, work, "push", "-q", "origin", "HEAD:main")
	os.WriteFile(filepath.Join(co.Dir, "a.js"), []byte("local edit\n"), 0o644)
	os.WriteFile(filepath.Join(co.Dir, "junk"), []byte("x"), 0o644)
	co2, err := m.Sync(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if co2.Commit != git(t, work, "rev-parse", "HEAD") || co2.Commit == co.Commit {
		t.Fatalf("not updated: %s", co2.Commit)
	}
	if b, _ := os.ReadFile(filepath.Join(co.Dir, "a.js")); string(b) != "v2\n" {
		t.Fatalf("a.js after sync = %q", b)
	}
	if _, err := os.Stat(filepath.Join(co.Dir, "junk")); !os.IsNotExist(err) {
		t.Fatal("untracked file not cleaned")
	}

	_, err = m.Sync(ctx, "missing")
	if err == nil || strings.Contains(err.Error(), "glpat-secret") {
		t.Fatalf("missing repo err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "repos", "grp", "missing")); !os.IsNotExist(err) {
		t.Fatal("failed clone left a directory behind")
	}
}

func TestEnvKeepsTokenOutOfArgs(t *testing.T) {
	m := New(Config{Token: "glpat-secret", CAFile: "/ca.pem"})
	env := strings.Join(m.env(), "\n")
	if !strings.Contains(env, "GIT_CONFIG_KEY_0=http.extraHeader") || !strings.Contains(env, "http.sslCAInfo") || strings.Contains(env, "sslVerify") {
		t.Errorf("env = %s", env)
	}
	m = New(Config{Token: "glpat-secret", CAFile: "/ca.pem", SkipTLSVerify: true})
	env = strings.Join(m.env(), "\n")
	if !strings.Contains(env, "GIT_CONFIG_VALUE_1=false") || strings.Contains(env, "sslCAInfo") {
		t.Errorf("env = %s", env)
	}
	if s := m.scrub("fatal: glpat-secret bad"); strings.Contains(s, "glpat-secret") {
		t.Errorf("scrub = %s", s)
	}
}
