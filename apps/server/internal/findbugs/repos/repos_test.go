package repos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
)

func TestContainer(t *testing.T) {
	for src, want := range map[string][2]string{
		"/var/log/pods/tdw-dev_service-payment-migration-5448bb778c-6sbnz_d75ca60c-f4b6-472e-b11b-f8d2e5f450bd/service-payment-migration/0.log": {"service-payment-migration", "tdw-dev"},
		"/var/log/pods/ns_pod-1_uid/app/12.log": {"app", "ns"},
		"/var/log/pods/ns_pod_uid/../etc/0.log": {"", ""},
		"/var/log/containers/x.log":             {"", ""},
		"":                                      {"", ""},
	} {
		if got := [2]string{Container(src), Namespace(src)}; got != want {
			t.Errorf("Container/Namespace(%q) = %q, want %q", src, got, want)
		}
	}
	evs := []splunk.Event{
		{Source: "/var/log/pods/n_p_u/b/0.log", Time: "2026-10-02T17:41:29.502+07:00"}, {Source: "/var/log/pods/n_p_u/a/0.log"},
		{Source: "/var/log/pods/n_p_u/b/0.log", Time: "2026-10-02T17:41:30.000+07:00"}, {Source: "other"},
		{Source: "/var/log/pods/m_p_u/c/0.log"}, {Source: "/var/log/pods/n_p_u/c/0.log"}, {Source: "/var/log/pods/n_p_u/c/0.log"},
	}
	if got := strings.Join(Containers(evs), ","); got != "b,c,a" {
		t.Errorf("Containers = %s", got)
	}
	ts := Targets(evs)
	if ts[0].Namespace != "n" || !ts[0].LastSeen.Equal(time.Date(2026, 10, 2, 10, 41, 30, 0, time.UTC)) || ts[1].Namespace != "n" || !ts[2].LastSeen.IsZero() {
		t.Errorf("Targets = %+v", ts)
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

// TestSync fetches from a local "GitLab" (bare repos under a directory
// served over file://) into one worktree per commit.
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
	git(t, work, "tag", "-a", "-m", "v1", "1.0.0")
	git(t, work, "push", "-q", "origin", "1.0.0")
	first := git(t, work, "rev-parse", "HEAD")

	repos := filepath.Join(root, "repos")
	m := New(Config{URL: "file://" + remote, Token: "glpat-secret", Dir: repos, Group: "grp"})
	ctx := context.Background()
	co, err := m.Sync(ctx, "svc", "")
	if err != nil {
		t.Fatal(err)
	}
	if co.Project != "grp/svc" || co.Ref != "main" || co.Commit != first || co.Dir != filepath.Join(repos, ".worktrees", "grp", "svc@"+first[:12]) {
		t.Fatalf("checkout = %+v", co)
	}
	if b, _ := os.ReadFile(filepath.Join(co.Dir, "a.js")); string(b) != "v1\n" {
		t.Fatalf("a.js = %q", b)
	}
	cfg, _ := os.ReadFile(filepath.Join(repos, "grp", "svc", ".git", "config"))
	if strings.Contains(string(cfg), "secret") || strings.Contains(string(cfg), "extraHeader") {
		t.Fatalf("credentials leaked into .git/config:\n%s", cfg)
	}

	os.WriteFile(filepath.Join(work, "a.js"), []byte("v2\n"), 0o644)
	git(t, work, "commit", "-qam", "two")
	git(t, work, "push", "-q", "origin", "HEAD:main")
	second := git(t, work, "rev-parse", "HEAD")
	co2, err := m.Sync(ctx, "svc", "main")
	if err != nil {
		t.Fatal(err)
	}
	if co2.Commit != second || co2.Dir == co.Dir {
		t.Fatalf("not updated: %+v", co2)
	}
	// The first commit's worktree is untouched by the newer sync.
	if b, _ := os.ReadFile(filepath.Join(co.Dir, "a.js")); string(b) != "v1\n" {
		t.Fatalf("old worktree a.js = %q", b)
	}

	// A deployed commit by SHA, and a tag, map to the same worktree.
	co3, err := m.Sync(ctx, "svc", first)
	if err != nil || co3.Dir != co.Dir || co3.Commit != first {
		t.Fatalf("sync by sha = %+v, %v", co3, err)
	}
	co4, err := m.Sync(ctx, "svc", "1.0.0")
	if err != nil || co4.Commit != first || co4.Ref != "1.0.0" {
		t.Fatalf("sync by tag = %+v, %v", co4, err)
	}

	for _, bad := range []string{"--upload-pack=x", "main..x", "HEAD~1", "a b"} {
		if _, err := m.Sync(ctx, "svc", bad); err == nil || !strings.Contains(err.Error(), "invalid ref") {
			t.Errorf("Sync(%q) err = %v", bad, err)
		}
	}
	_, err = m.Sync(ctx, "missing", "")
	if err == nil || strings.Contains(err.Error(), "glpat-secret") {
		t.Fatalf("missing repo err = %v", err)
	}

	// Prune drops worktrees unused for the TTL unless kept.
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(co.Dir, old, old)
	os.Chtimes(co2.Dir, old, old)
	n, err := m.Prune(ctx, 24*time.Hour, func(dir string) bool { return dir == co2.Dir })
	if err != nil || n != 1 {
		t.Fatalf("Prune = %d, %v", n, err)
	}
	if _, err := os.Stat(co.Dir); !os.IsNotExist(err) {
		t.Fatal("stale worktree not removed")
	}
	if _, err := os.Stat(co2.Dir); err != nil {
		t.Fatal("kept worktree removed")
	}
	// A pruned commit can be checked out again.
	if co5, err := m.Sync(ctx, "svc", first); err != nil || co5.Dir != co.Dir {
		t.Fatalf("re-sync after prune = %+v, %v", co5, err)
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

// TestCloneAndList clones a project in full, lists it next to a tracer
// store, and checks a later Sync does not make the clone shallow.
func TestCloneAndList(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "gitlab")
	work := filepath.Join(root, "work")
	git(t, root, "init", "-q", "--bare", "-b", "main", filepath.Join(remote, "grp", "svc.git"))
	git(t, root, "clone", "-q", filepath.Join(remote, "grp", "svc.git"), work)
	for _, v := range []string{"one", "two"} {
		os.WriteFile(filepath.Join(work, "a.js"), []byte(v), 0o644)
		git(t, work, "add", ".")
		git(t, work, "commit", "-qm", v)
	}
	git(t, work, "push", "-q", "origin", "HEAD:main")
	git(t, root, "init", "-q", "--bare", "-b", "main", filepath.Join(remote, "grp", "sub", "other.git"))
	git(t, work, "push", "-q", filepath.Join(remote, "grp", "sub", "other.git"), "HEAD:main")

	repos := filepath.Join(root, "repos")
	m := New(Config{URL: "file://" + remote, Token: "glpat-secret", Dir: repos, Group: "grp"})
	ctx := context.Background()

	if l, err := m.List(ctx); err != nil || len(l) != 0 {
		t.Fatalf("List of missing dir = %+v, %v", l, err)
	}
	dir, err := m.Clone(ctx, "svc")
	if err != nil || dir != filepath.Join(repos, "grp", "svc") {
		t.Fatalf("Clone = %s, %v", dir, err)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if strings.Contains(string(cfg), "secret") || strings.Contains(string(cfg), "extraHeader") {
		t.Fatalf("credentials leaked into .git/config:\n%s", cfg)
	}
	if _, err := m.Clone(ctx, "grp/svc"); err != ErrExists {
		t.Fatalf("second Clone err = %v", err)
	}
	for _, bad := range []string{"../x", "grp/.hidden", "a b", ""} {
		if _, err := m.Clone(ctx, bad); err != ErrInvalidProject {
			t.Errorf("Clone(%q) err = %v", bad, err)
		}
	}
	// A nested group, and a tracer worktree that List must skip.
	if _, err := m.Clone(ctx, "grp/sub/other.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(ctx, "svc", ""); err != nil {
		t.Fatal(err)
	}
	if git(t, dir, "rev-parse", "--is-shallow-repository") != "false" {
		t.Fatal("Sync made the full clone shallow")
	}
	l, err := m.List(ctx)
	if err != nil || len(l) != 2 {
		t.Fatalf("List = %+v, %v", l, err)
	}
	if l[0].Project != "grp/sub/other" || l[1].Project != "grp/svc" || l[1].Branch != "main" ||
		l[1].Subject != "two" || l[1].Shallow || l[1].CommittedAt.IsZero() || len(l[1].Commit) != 40 {
		t.Fatalf("List = %+v", l)
	}
	if entries, _ := os.ReadDir(filepath.Join(repos, "grp")); len(entries) != 2 {
		t.Fatalf("leftover temp dirs: %v", entries)
	}
}
