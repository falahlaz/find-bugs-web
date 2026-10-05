// Package configrepo checks out the JSON config an environment ran with.
// The services' own assets/*.json files are for local development only; on
// the servers they come from ConfigMaps built from one config repo, one
// branch per environment. A trace reads the commit of that repo deployed
// to the environment's ConfigMaps when the error happened, since the
// branch may have been fixed since.
package configrepo

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/analyzer"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/redact"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

// Repos is the subset of repos.Manager used here.
type Repos interface {
	FetchBranches(ctx context.Context, project string, branches []string) error
	CommitBefore(ctx context.Context, project, branch string, t time.Time) (string, error)
	SparseCheckout(ctx context.Context, project, commit string, paths []string) (repos.Checkout, error)
}

// Deployments is the subset of gitlab.Client used here.
type Deployments interface {
	BranchDeployment(ctx context.Context, project, ref, jobPrefix string, before time.Time) (gitlab.Deployment, error)
}

// Config names the config repo.
type Config struct {
	Project string // GitLab group/project
	Path    string // directory of the JSON files in the repo
	// Branches maps a GitLab environment to its branch; an environment not
	// in it uses the branch of the same name.
	Branches map[string]string
	// JobPrefix starts the names of the CI jobs that deploy a branch to the
	// ConfigMaps.
	JobPrefix string
}

// Branch returns the config branch of a GitLab environment.
func (c Config) Branch(env string) string {
	if b := c.Branches[env]; b != "" {
		return b
	}
	return env
}

// Source checks out versions of the config repo.
type Source struct {
	Cfg     Config
	Repos   Repos
	Deploys Deployments
}

// Checkout is the config of one environment at one commit.
type Checkout struct {
	Version store.ConfigVersion
	// Repo is the sparse worktree holding only Version.Path.
	Repo store.TraceRepo
}

// Checkout pulls env's branch, picks the commit deployed to its ConfigMaps
// before before (the latest one when before is zero) and checks out the
// JSON files of that commit. When no deployment is found it falls back to
// the last commit on the branch before before, and says so in the note.
func (s *Source) Checkout(ctx context.Context, env string, before time.Time) (Checkout, error) {
	branch := s.Cfg.Branch(env)
	v := store.ConfigVersion{Project: s.Cfg.Project, Env: env, Branch: branch, Path: s.Cfg.Path}
	if err := s.Repos.FetchBranches(ctx, s.Cfg.Project, []string{branch}); err != nil {
		return Checkout{Version: v}, fmt.Errorf("pull branch %s: %w", branch, err)
	}
	sha := ""
	if s.Deploys != nil {
		dep, err := s.Deploys.BranchDeployment(ctx, s.Cfg.Project, branch, s.Cfg.JobPrefix, before)
		switch {
		case err == nil:
			sha = dep.SHA
			v.Source = store.RefDeployed
			v.DeployJobURL = dep.JobURL
			if !dep.FinishedAt.IsZero() {
				v.DeployedAt = dep.FinishedAt.UTC().Format(time.RFC3339)
			}
		case errors.Is(err, gitlab.ErrNotFound):
			v.Note = fmt.Sprintf("Tidak ada job %s* sukses di branch %s sebelum waktu error; dipakai commit terakhir sebelum waktu error.", s.Cfg.JobPrefix, branch)
		default:
			v.Note = "Gagal membaca deployment config dari GitLab (" + redact.Sensitive(err.Error()) + "); dipakai commit terakhir sebelum waktu error."
		}
	}
	if sha == "" {
		c, err := s.Repos.CommitBefore(ctx, s.Cfg.Project, branch, before)
		if err != nil {
			return Checkout{Version: v}, err
		}
		sha = c
		v.Source = store.RefFallback
		if v.Note == "" {
			v.Note = "Dipakai commit terakhir di branch sebelum waktu error."
		}
	}
	co, err := s.Repos.SparseCheckout(ctx, s.Cfg.Project, sha, []string{s.Cfg.Path})
	if err != nil {
		return Checkout{Version: v}, err
	}
	v.Commit = co.Commit
	return Checkout{
		Version: v,
		Repo: store.TraceRepo{
			Kind: store.RepoConfig, Project: s.Cfg.Project, Dir: co.Dir, Path: s.Cfg.Path,
			Commit: co.Commit, Ref: branch, RefSource: v.Source, Env: env,
		},
	}, nil
}

// Repo is a config checkout as the tracer sees it: the directory of the
// JSON files, labelled with the environment and version.
func Repo(r store.TraceRepo) analyzer.Repo {
	return analyzer.Repo{
		Project: r.Project, Dir: filepath.Join(r.Dir, filepath.FromSlash(r.Path)), Commit: r.Commit, Ref: r.Ref, Env: r.Env,
		Config: true, Deployed: r.RefSource == store.RefDeployed,
	}
}
