package configrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

type fakeRepos struct {
	fetched  []string
	before   map[string]string // branch -> commit before the asked time
	fetchErr error
}

func (f *fakeRepos) FetchBranches(_ context.Context, _ string, branches []string) error {
	f.fetched = append(f.fetched, branches...)
	return f.fetchErr
}

func (f *fakeRepos) CommitBefore(_ context.Context, _, branch string, _ time.Time) (string, error) {
	if c := f.before[branch]; c != "" {
		return c, nil
	}
	return "", errors.New("no commit")
}

func (f *fakeRepos) SparseCheckout(_ context.Context, project, commit string, paths []string) (repos.Checkout, error) {
	return repos.Checkout{Project: project, Commit: commit, Dir: "/w/.worktrees/" + project + "@" + commit[:4]}, nil
}

type fakeDeploys struct {
	byBranch map[string]gitlab.Deployment
	err      error
	before   time.Time
}

func (f *fakeDeploys) BranchDeployment(_ context.Context, _, ref, prefix string, before time.Time) (gitlab.Deployment, error) {
	f.before = before
	if f.err != nil {
		return gitlab.Deployment{}, f.err
	}
	if d, ok := f.byBranch[ref]; ok && prefix == "deploy_configmaps" {
		return d, nil
	}
	return gitlab.Deployment{}, fmt.Errorf("none: %w", gitlab.ErrNotFound)
}

func TestCheckout(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 5, 9, 59, 24, 0, time.UTC)
	rs := &fakeRepos{before: map[string]string{"dev": "aaaa1111", "tdw-blue": "bbbb2222"}}
	ds := &fakeDeploys{byBranch: map[string]gitlab.Deployment{
		"dev": {SHA: "ba0a8a5c", Ref: "dev", JobURL: "https://g/jobs/2", FinishedAt: at.Add(-5 * time.Minute)},
	}}
	src := &Source{
		Cfg:   Config{Project: "ops/cfg", Path: "json-files", Branches: map[string]string{"blue": "tdw-blue"}, JobPrefix: "deploy_configmaps"},
		Repos: rs, Deploys: ds,
	}

	// The deployed commit, pulled first.
	co, err := src.Checkout(ctx, "dev", at)
	if err != nil {
		t.Fatal(err)
	}
	v := co.Version
	if v.Commit != "ba0a8a5c" || v.Source != store.RefDeployed || v.Branch != "dev" || v.Env != "dev" || v.DeployedAt != "2026-10-05T09:54:24Z" || v.Note != "" {
		t.Fatalf("version = %+v", v)
	}
	if !ds.before.Equal(at) || strings.Join(rs.fetched, ",") != "dev" {
		t.Fatalf("before = %v, fetched = %v", ds.before, rs.fetched)
	}
	if r := co.Repo; r.Kind != store.RepoConfig || r.Path != "json-files" || r.Ref != "dev" || r.RefSource != store.RefDeployed {
		t.Fatalf("repo = %+v", r)
	}
	if ar := Repo(co.Repo); !ar.Config || !ar.Deployed || ar.Dir != co.Repo.Dir+"/json-files" {
		t.Fatalf("analyzer repo = %+v", ar)
	}

	// No deployment: the last commit before the error, on the mapped branch.
	co, err = src.Checkout(ctx, "blue", at)
	if err != nil || co.Version.Commit != "bbbb2222" || co.Version.Branch != "tdw-blue" || co.Version.Source != store.RefFallback ||
		!strings.Contains(co.Version.Note, "deploy_configmaps*") {
		t.Fatalf("fallback = %+v, %v", co.Version, err)
	}
	if Repo(co.Repo).Deployed {
		t.Fatal("fallback marked deployed")
	}

	// The GitLab API failing also falls back, and says so.
	ds.err = errors.New("HTTP 500")
	if co, err := src.Checkout(ctx, "dev", at); err != nil || co.Version.Commit != "aaaa1111" || !strings.Contains(co.Version.Note, "HTTP 500") {
		t.Fatalf("api error fallback = %+v, %v", co.Version, err)
	}

	// Pull failures are errors.
	rs.fetchErr = errors.New("vpn down")
	if _, err := src.Checkout(ctx, "dev", at); err == nil || !strings.Contains(err.Error(), "vpn down") {
		t.Fatalf("fetch error = %v", err)
	}
}

func TestBranch(t *testing.T) {
	c := Config{Branches: map[string]string{"blue": "tdw-blue"}}
	if c.Branch("blue") != "tdw-blue" || c.Branch("production") != "production" {
		t.Fatal("branch mapping")
	}
}
