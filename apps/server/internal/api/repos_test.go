package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/rcsession"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/config"
)

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestRepos(t *testing.T) {
	h := newHarness(t)
	qa, eng := h.login("qa1"), h.login("eng")
	root := t.TempDir()
	remote := filepath.Join(root, "gitlab")
	work := filepath.Join(root, "work")
	gitCmd(t, root, "init", "-q", "--bare", "-b", "main", filepath.Join(remote, "grp", "svc.git"))
	gitCmd(t, root, "clone", "-q", filepath.Join(remote, "grp", "svc.git"), work)
	gitCmd(t, work, "commit", "-q", "--allow-empty", "-m", "first")
	gitCmd(t, work, "push", "-q", "origin", "HEAD:main")

	dir := filepath.Join(root, "repos")
	h.api.Cfg.GitLab = config.GitLab{URL: "file://" + remote, Token: "glpat-secret", Group: "grp"}
	h.api.Repos = repos.New(repos.Config{URL: "file://" + remote, Token: "glpat-secret", Dir: dir, Group: "grp"})
	script, _ := filepath.Abs("../findbugs/rcsession/testdata/fake-rc-session.sh")
	tmux, _ := filepath.Abs("../findbugs/rcsession/testdata/fake-tmux.sh")
	h.api.RC = rcsession.New(rcsession.Config{Script: script, Root: dir, Tmux: tmux})

	if code := qa.do("GET", "/api/repos", nil, nil); code != 403 {
		t.Fatalf("QA list = %d", code)
	}
	if code := eng.do("POST", "/api/repos", RepoRequest{Project: "../etc"}, nil); code != 400 {
		t.Fatalf("bad clone = %d", code)
	}
	var cl RepoCloneView
	if code := eng.do("POST", "/api/repos", RepoRequest{Project: "svc"}, &cl); code != 202 || cl.Project != "grp/svc" || cl.State != "cloning" {
		t.Fatalf("clone = %d %+v", code, cl)
	}
	var list ReposResponse
	for i := 0; ; i++ {
		eng.do("GET", "/api/repos", nil, &list)
		if len(list.Clones) == 0 && len(list.Repos) == 1 {
			break
		}
		if i > 200 {
			t.Fatalf("clone did not finish: %+v", list)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if r := list.Repos[0]; r.Project != "grp/svc" || r.Branch != "main" || r.Subject != "first" || r.Session != nil || !list.CanClone || !list.CanSession {
		t.Fatalf("list = %+v", list)
	}
	if code := eng.do("POST", "/api/repos", RepoRequest{Project: "grp/svc"}, nil); code != 409 {
		t.Fatalf("duplicate clone = %d", code)
	}
	var failed RepoCloneView
	eng.do("POST", "/api/repos", RepoRequest{Project: "missing"}, nil)
	for i := 0; failed.State != "failed"; i++ {
		eng.do("GET", "/api/repos", nil, &list)
		if len(list.Clones) == 1 {
			failed = list.Clones[0]
		}
		if i > 200 {
			t.Fatalf("failed clone not reported: %+v", list)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if strings.Contains(failed.Error, "glpat-secret") || failed.By != "eng" {
		t.Fatalf("failed clone = %+v", failed)
	}

	var sr RepoSessionResponse
	if code := eng.do("POST", "/api/repos/session/start", RepoRequest{Project: "grp/missing"}, nil); code != 404 {
		t.Fatalf("start unknown = %d", code)
	}
	if code := eng.do("POST", "/api/repos/session/start", RepoRequest{Project: "grp/svc"}, &sr); code != 200 || !strings.Contains(sr.Output, "Connected") {
		t.Fatalf("start = %d %+v", code, sr)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".fake-log")); !strings.Contains(string(b), "args: start "+filepath.Join(dir, "grp", "svc")+"\n") {
		t.Fatalf("script log:\n%s", b)
	}
	if code := eng.do("POST", "/api/repos/session/stop", RepoRequest{Project: "grp/svc"}, &sr); code != 200 {
		t.Fatalf("stop = %d %+v", code, sr)
	}

	var audit AuditListResponse
	eng.do("GET", "/api/audit", nil, &audit)
	actions := []string{}
	for _, e := range audit.Entries {
		actions = append(actions, e.Action+":"+e.Result)
	}
	for _, want := range []string{"repo.clone:ok", "repo.clone:failed", "repo.session_start:ok", "repo.session_stop:ok"} {
		if !strings.Contains(strings.Join(actions, ","), want) {
			t.Errorf("audit missing %s: %v", want, actions)
		}
	}
}
