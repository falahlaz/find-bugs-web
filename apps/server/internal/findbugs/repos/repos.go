// Package repos keeps local checkouts of the GitLab repos of the services
// that logged a transaction, so the analyzer can trace an internal error to
// the code. A service is found from the Kubernetes container name in the
// Splunk source path and maps to the GitLab project <group>/<container>
// unless overridden.
package repos

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/redact"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
)

// podLogPath matches the kubelet log path Splunk reports as source:
// /var/log/pods/<namespace>_<pod>_<uid>/<container>/<n>.log
var podLogPath = regexp.MustCompile(`^/var/log/pods/([a-z0-9]([a-z0-9-]*[a-z0-9])?)_([^/_]+)_[^/]+/([a-z0-9]([a-z0-9-]*[a-z0-9])?)/[0-9]+\.log$`)

// Container returns the Kubernetes container name in a Splunk source path,
// or "" if source is not a pod log path.
func Container(source string) string {
	if m := podLogPath.FindStringSubmatch(source); m != nil {
		return m[4]
	}
	return ""
}

// Pod returns "<namespace>_<pod>" from a Splunk source path, or "" if
// source is not a pod log path.
func Pod(source string) string {
	if m := podLogPath.FindStringSubmatch(source); m != nil {
		return m[1] + "_" + m[3]
	}
	return ""
}

// Pods lists the pods that logged events, most events first (ties by name).
func Pods(events []splunk.Event) []string {
	n := map[string]int{}
	var out []string
	for _, ev := range events {
		p := Pod(ev.Source)
		if p == "" {
			continue
		}
		if n[p] == 0 {
			out = append(out, p)
		}
		n[p]++
	}
	sort.Slice(out, func(i, j int) bool {
		if n[out[i]] != n[out[j]] {
			return n[out[i]] > n[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// Namespace returns the Kubernetes namespace in a Splunk source path, or ""
// if source is not a pod log path.
func Namespace(source string) string {
	if m := podLogPath.FindStringSubmatch(source); m != nil {
		return m[1]
	}
	return ""
}

// Target is a service that logged events of a transaction: its container,
// the namespace (environment) it ran in and when it last logged.
type Target struct {
	Container string
	Namespace string
	LastSeen  time.Time // zero when no event time could be parsed
	events    int
}

// Targets lists the containers that logged events, most events first (ties
// by name). A container seen in several namespaces is reported for the one
// it logged most in.
func Targets(events []splunk.Event) []Target {
	byNS := map[[2]string]*Target{}
	for _, ev := range events {
		c := Container(ev.Source)
		if c == "" {
			continue
		}
		k := [2]string{c, Namespace(ev.Source)}
		t := byNS[k]
		if t == nil {
			t = &Target{Container: c, Namespace: k[1]}
			byNS[k] = t
		}
		t.events++
		if at, ok := eventTime(ev.Time); ok && at.After(t.LastSeen) {
			t.LastSeen = at
		}
	}
	best := map[string]*Target{}
	for _, t := range byNS {
		if b := best[t.Container]; b == nil || t.events > b.events || (t.events == b.events && t.Namespace < b.Namespace) {
			best[t.Container] = t
		}
	}
	out := make([]Target, 0, len(best))
	for _, t := range best {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].events != out[j].events {
			return out[i].events > out[j].events
		}
		return out[i].Container < out[j].Container
	})
	return out
}

// Containers lists the containers that logged events, most events first
// (ties by name).
func Containers(events []splunk.Event) []string {
	ts := Targets(events)
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Container
	}
	return out
}

// eventTime parses a Splunk _time value (RFC 3339, usually with
// milliseconds and an offset).
func eventTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

var projectPath = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`)

// validProject rejects paths that could escape the repos directory.
func validProject(p string) bool {
	if !projectPath.MatchString(p) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

// Config configures a Manager.
type Config struct {
	URL           string // GitLab base URL, e.g. https://gitlab.example.com
	Username      string
	Token         string
	CAFile        string
	SkipTLSVerify bool
	// Dir holds one shallow repo per project, <Dir>/<group>/<project>, used
	// as an object store, and the checkouts made from it under
	// <Dir>/.worktrees/<group>/<project>@<commit>.
	Dir     string
	Group   string            // default group for a container
	RepoMap map[string]string // container -> group/project overrides
	Ref     string            // branch to check out when no other is asked for
	Timeout time.Duration     // per git command
	// CloneTimeout bounds a full clone made with Clone (Timeout otherwise).
	CloneTimeout time.Duration
}

// Checkout is a read-only worktree of one commit of a project.
type Checkout struct {
	Container string
	Project   string // group/project
	Dir       string
	Ref       string // what was asked for: a branch, tag or commit
	Commit    string
}

// Manager clones and updates repos. It is safe for concurrent use; work on
// one repo is serialised.
type Manager struct {
	cfg   Config
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New returns a Manager.
func New(cfg Config) *Manager {
	if cfg.Ref == "" {
		cfg.Ref = "main"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Minute
	}
	if cfg.Username == "" {
		cfg.Username = "oauth2"
	}
	if cfg.CloneTimeout <= 0 {
		cfg.CloneTimeout = 15 * time.Minute
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return &Manager{cfg: cfg, locks: map[string]*sync.Mutex{}}
}

// DefaultRef is the branch checked out when no deployed commit is known.
func (m *Manager) DefaultRef() string { return m.cfg.Ref }

// Project returns the GitLab project for a container.
func (m *Manager) Project(container string) string {
	if p := m.cfg.RepoMap[container]; p != "" {
		return p
	}
	return m.cfg.Group + "/" + container
}

func (m *Manager) lock(project string) func() {
	m.mu.Lock()
	l := m.locks[project]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[project] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

var (
	fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// refName is a conservative branch/tag name: no option-like or
	// revision-syntax names reach git.
	refName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]*$`)
)

// worktreesDir is where checkouts live, inside Dir. Project paths never
// start with a dot, so it cannot clash with a group.
const worktreesDir = ".worktrees"

// Sync makes a worktree of ref (a branch or tag, Config.Ref when empty, or
// a full commit SHA) of the container's repo and returns it. Each commit
// gets its own worktree that is never changed afterwards, so a checkout
// handed to a tracer stays put while other jobs sync other commits.
func (m *Manager) Sync(ctx context.Context, container, ref string) (Checkout, error) {
	if ref == "" {
		ref = m.cfg.Ref
	}
	project := m.Project(container)
	co := Checkout{Container: container, Project: project, Ref: ref}
	if !validProject(project) {
		return co, fmt.Errorf("invalid GitLab project %q", project)
	}
	if !fullSHA.MatchString(ref) && (!refName.MatchString(ref) || strings.Contains(ref, "..")) {
		return co, fmt.Errorf("invalid ref %q", ref)
	}
	store := filepath.Join(m.cfg.Dir, filepath.FromSlash(project))
	defer m.lock(project)()

	if err := m.initStore(ctx, store); err != nil {
		return co, err
	}
	commit := ref
	if !fullSHA.MatchString(ref) || !m.hasCommit(ctx, store, ref) {
		remote := m.cfg.URL + "/" + project + ".git"
		args := []string{"fetch", "--quiet", "--no-tags"}
		// A full clone made with Clone stays full; a depth would cut its
		// history.
		if !m.isFullClone(ctx, store) {
			args = append(args, "--depth", "1")
		}
		if _, err := m.git(ctx, store, append(args, remote, ref)...); err != nil {
			return co, err
		}
		out, err := m.git(ctx, store, "rev-parse", "FETCH_HEAD^{commit}")
		if err != nil {
			return co, err
		}
		commit = strings.TrimSpace(out)
	}
	co.Commit = commit
	co.Dir = m.worktreePath(project, commit)
	if out, err := m.git(ctx, co.Dir, "rev-parse", "HEAD"); err == nil && strings.TrimSpace(out) == commit {
		// Already there; mark it used for Prune.
		now := time.Now()
		_ = os.Chtimes(co.Dir, now, now)
		return co, nil
	}
	// A leftover directory from an interrupted add would block git.
	_ = os.RemoveAll(co.Dir)
	if _, err := m.git(ctx, store, "worktree", "prune"); err != nil {
		return co, err
	}
	if err := os.MkdirAll(filepath.Dir(co.Dir), 0o755); err != nil {
		return co, err
	}
	if _, err := m.git(ctx, store, "worktree", "add", "--quiet", "--detach", co.Dir, commit); err != nil {
		_ = os.RemoveAll(co.Dir)
		return co, err
	}
	return co, nil
}

// FetchBranches brings branches of project up to date in a blobless store
// at <Dir>/<project>: commits and trees only, file contents are fetched
// when a SparseCheckout needs them. Each branch lands in
// refs/remotes/origin/<branch>. Used for repos too big to check out whole,
// like the JSON config repo.
func (m *Manager) FetchBranches(ctx context.Context, project string, branches []string) error {
	if !validProject(project) {
		return fmt.Errorf("invalid GitLab project %q", project)
	}
	if len(branches) == 0 {
		return errors.New("no branches to fetch")
	}
	args := []string{"fetch", "--quiet", "--no-tags", "--filter=blob:none", "origin"}
	for _, b := range branches {
		if !refName.MatchString(b) || strings.Contains(b, "..") {
			return fmt.Errorf("invalid branch %q", b)
		}
		args = append(args, fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", b, b))
	}
	store := filepath.Join(m.cfg.Dir, filepath.FromSlash(project))
	defer m.lock(project)()
	if err := m.initPartialStore(ctx, store, project); err != nil {
		return err
	}
	_, err := m.git(ctx, store, args...)
	return err
}

// initPartialStore creates a blobless store whose origin remote is marked
// as a promisor, so missing file contents are fetched on demand. The remote
// URL holds no credentials; they come from the environment as for Sync.
func (m *Manager) initPartialStore(ctx context.Context, store, project string) error {
	if err := m.initStore(ctx, store); err != nil {
		return err
	}
	if _, err := m.git(ctx, store, "config", "--get", "remote.origin.url"); err == nil {
		return nil
	}
	for _, kv := range [][]string{
		{"remote", "add", "origin", m.cfg.URL + "/" + project + ".git"},
		{"config", "remote.origin.promisor", "true"},
		{"config", "remote.origin.partialclonefilter", "blob:none"},
	} {
		if _, err := m.git(ctx, store, kv...); err != nil {
			return err
		}
	}
	return nil
}

// CommitBefore returns the last commit on a branch fetched with
// FetchBranches committed at or before t (the tip when t is zero).
func (m *Manager) CommitBefore(ctx context.Context, project, branch string, t time.Time) (string, error) {
	if !validProject(project) || !refName.MatchString(branch) || strings.Contains(branch, "..") {
		return "", fmt.Errorf("invalid project %q or branch %q", project, branch)
	}
	args := []string{"rev-list", "-1"}
	if !t.IsZero() {
		args = append(args, fmt.Sprintf("--before=%d", t.Unix()))
	}
	store := filepath.Join(m.cfg.Dir, filepath.FromSlash(project))
	defer m.lock(project)()
	out, err := m.git(ctx, store, append(args, "refs/remotes/origin/"+branch, "--")...)
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("no commit on %s before %s", branch, t.Format(time.RFC3339))
	}
	return sha, nil
}

// SparseCheckout makes a worktree of commit in project's blobless store
// holding only paths (directories relative to the repo root), and returns
// it. Like Sync, each commit gets its own worktree that never changes.
func (m *Manager) SparseCheckout(ctx context.Context, project, commit string, paths []string) (Checkout, error) {
	co := Checkout{Project: project, Ref: commit, Commit: commit}
	if !validProject(project) {
		return co, fmt.Errorf("invalid GitLab project %q", project)
	}
	if !fullSHA.MatchString(commit) {
		return co, fmt.Errorf("invalid commit %q", commit)
	}
	patterns := []string{"set", "--no-cone"}
	for _, p := range paths {
		p = strings.Trim(filepath.ToSlash(p), "/")
		if p == "" || !validProject("x/"+p) {
			return co, fmt.Errorf("invalid path %q", p)
		}
		patterns = append(patterns, "/"+p+"/")
	}
	store := filepath.Join(m.cfg.Dir, filepath.FromSlash(project))
	defer m.lock(project)()
	if err := m.initPartialStore(ctx, store, project); err != nil {
		return co, err
	}
	if !m.hasCommit(ctx, store, commit) {
		if _, err := m.git(ctx, store, "fetch", "--quiet", "--no-tags", "--filter=blob:none", "origin", commit); err != nil {
			return co, err
		}
	}
	co.Dir = m.worktreePath(project, commit)
	if out, err := m.git(ctx, co.Dir, "rev-parse", "HEAD"); err == nil && strings.TrimSpace(out) == commit {
		now := time.Now()
		_ = os.Chtimes(co.Dir, now, now)
		return co, nil
	}
	_ = os.RemoveAll(co.Dir)
	if _, err := m.git(ctx, store, "worktree", "prune"); err != nil {
		return co, err
	}
	if err := os.MkdirAll(filepath.Dir(co.Dir), 0o755); err != nil {
		return co, err
	}
	fail := func(err error) (Checkout, error) {
		_ = os.RemoveAll(co.Dir)
		_, _ = m.git(ctx, store, "worktree", "prune")
		return co, err
	}
	if _, err := m.git(ctx, store, "worktree", "add", "--quiet", "--no-checkout", "--detach", co.Dir, commit); err != nil {
		return fail(err)
	}
	if _, err := m.git(ctx, co.Dir, append([]string{"sparse-checkout"}, patterns...)...); err != nil {
		return fail(err)
	}
	// Fetches the missing file contents of paths in one batch.
	if _, err := m.git(ctx, co.Dir, "checkout", "--quiet"); err != nil {
		return fail(err)
	}
	return co, nil
}

// initStore creates the project's object store if needed. Older versions
// kept a clone with a checkout of main there; it works as a store as is.
func (m *Manager) initStore(ctx context.Context, store string) error {
	if _, err := os.Stat(filepath.Join(store, ".git")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// A leftover directory from an interrupted init would block git.
	_ = os.RemoveAll(store)
	if err := os.MkdirAll(store, 0o755); err != nil {
		return err
	}
	if _, err := m.git(ctx, store, "init", "--quiet"); err != nil {
		_ = os.RemoveAll(store)
		return err
	}
	return nil
}

// isFullClone reports whether dir has a checked-out branch with its whole
// history. A store made by initStore has no HEAD commit, so it is not one
// even though git does not call it shallow.
func (m *Manager) isFullClone(ctx context.Context, dir string) bool {
	if _, err := m.git(ctx, dir, "rev-parse", "--verify", "-q", "HEAD^{commit}"); err != nil {
		return false
	}
	out, err := m.git(ctx, dir, "rev-parse", "--is-shallow-repository")
	return err == nil && strings.TrimSpace(out) == "false"
}

func (m *Manager) hasCommit(ctx context.Context, store, sha string) bool {
	_, err := m.git(ctx, store, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

func (m *Manager) worktreePath(project, commit string) string {
	return filepath.Join(m.cfg.Dir, worktreesDir, filepath.FromSlash(project)+"@"+commit[:12])
}

// Prune removes worktrees not used (synced) for ttl, except those keep
// reports as still needed. It returns how many it removed.
func (m *Manager) Prune(ctx context.Context, ttl time.Duration, keep func(dir string) bool) (int, error) {
	if ttl <= 0 {
		return 0, nil
	}
	root := filepath.Join(m.cfg.Dir, worktreesDir)
	cutoff := time.Now().Add(-ttl)
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() && p != root && strings.Contains(d.Name(), "@") {
			dirs = append(dirs, p)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, dir := range dirs {
		fi, err := os.Stat(dir)
		if err != nil || !fi.ModTime().Before(cutoff) || (keep != nil && keep(dir)) {
			continue
		}
		rel, _ := filepath.Rel(root, dir)
		project := filepath.ToSlash(rel[:strings.LastIndex(rel, "@")])
		unlock := m.lock(project)
		if err := os.RemoveAll(dir); err == nil {
			n++
		}
		if store := filepath.Join(m.cfg.Dir, filepath.FromSlash(project)); validProject(project) {
			_, _ = m.git(ctx, store, "worktree", "prune")
		}
		unlock()
	}
	return n, nil
}

// Repo is a repository under Dir: a store the tracer fetches into, or a
// full clone made with Clone.
type Repo struct {
	Project     string // group/project
	Dir         string
	Branch      string // "" when HEAD is detached or unborn
	Commit      string
	Subject     string
	CommittedAt time.Time
	Shallow     bool
}

// List returns the repos under Dir, sorted by project. Directories starting
// with a dot (the worktrees, clones in progress) are skipped.
func (m *Manager) List(ctx context.Context) ([]Repo, error) {
	const maxDepth = 4
	var out []Repo
	root := filepath.Clean(m.cfg.Dir)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if p == root && errors.Is(err, os.ErrNotExist) {
				return filepath.SkipAll
			}
			return nil
		}
		if !d.IsDir() || p == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, p)
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			out = append(out, m.describe(ctx, filepath.ToSlash(rel), p))
			return filepath.SkipDir
		}
		if strings.Count(rel, string(filepath.Separator)) >= maxDepth-1 {
			return filepath.SkipDir
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return out, err
}

// describe reads a repo's branch and last commit; what cannot be read is
// left empty.
func (m *Manager) describe(ctx context.Context, project, dir string) Repo {
	r := Repo{Project: project, Dir: dir}
	if out, err := m.git(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		r.Branch = strings.TrimSpace(out)
	}
	if out, err := m.git(ctx, dir, "log", "-1", "--format=%H%x00%s%x00%cI"); err == nil {
		if f := strings.SplitN(strings.TrimSpace(out), "\x00", 3); len(f) == 3 {
			r.Commit, r.Subject = f[0], f[1]
			r.CommittedAt, _ = time.Parse(time.RFC3339, f[2])
		}
	}
	if out, err := m.git(ctx, dir, "rev-parse", "--is-shallow-repository"); err == nil {
		r.Shallow = strings.TrimSpace(out) == "true"
	}
	return r
}

// ErrNotCloned is returned by MergeConflicts for a project that has no
// repo under Dir.
var ErrNotCloned = errors.New("repo belum di-clone")

// MergeConflicts fetches the source and target branches of a merge request
// into the project's repo under Dir and returns the files that conflict
// when source is merged into target, without touching the working tree:
// git merge-tree merges in memory. A shallow repo (a tracing store) is
// unshallowed first, since the merge needs the common ancestor.
func (m *Manager) MergeConflicts(ctx context.Context, project, source, target string) ([]string, error) {
	if !validProject(project) {
		return nil, fmt.Errorf("invalid GitLab project %q", project)
	}
	for _, b := range []string{source, target} {
		if !refName.MatchString(b) || strings.Contains(b, "..") {
			return nil, fmt.Errorf("invalid branch %q", b)
		}
	}
	dir := filepath.Join(m.cfg.Dir, filepath.FromSlash(project))
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return nil, ErrNotCloned
	}
	defer m.lock(project)()
	// Fetch by URL into a namespace of our own: tracing stores have no
	// origin remote, and the refs of a working clone stay untouched.
	const ns = "refs/mr-triage/"
	args := []string{"fetch", "--quiet", "--no-tags", "--force"}
	if out, err := m.git(ctx, dir, "rev-parse", "--is-shallow-repository"); err == nil && strings.TrimSpace(out) == "true" {
		args = append(args, "--unshallow")
	}
	args = append(args, m.cfg.URL+"/"+project+".git",
		"refs/heads/"+source+":"+ns+"source", "refs/heads/"+target+":"+ns+"target")
	if _, err := m.gitTimeout(ctx, m.cfg.CloneTimeout, dir, args...); err != nil {
		return nil, err
	}
	out, err := m.git(ctx, dir, "merge-tree", "--write-tree", "--name-only", "--no-messages", ns+"target", ns+"source")
	var exit *exec.ExitError
	switch {
	case err == nil:
		return []string{}, nil
	case !errors.As(err, &exit) || exit.ExitCode() != 1:
		return nil, err
	}
	// Exit 1: the tree OID, then one conflicted path per line.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	files := []string{}
	for _, l := range lines[1:] {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// ErrExists is returned by Clone when the project is already under Dir.
var ErrExists = errors.New("repo sudah ada")

// ErrInvalidProject is returned by Clone for a name that is not a GitLab
// project path.
var ErrInvalidProject = errors.New("nama project tidak valid")

// ProjectPath turns a bare project name into <Group>/<name>; a path with a
// group is returned as is. It reports whether the result is a valid path.
func (m *Manager) ProjectPath(name string) (string, bool) {
	name = strings.Trim(strings.TrimSpace(name), "/")
	name = strings.TrimSuffix(name, ".git")
	if !strings.Contains(name, "/") {
		name = m.cfg.Group + "/" + name
	}
	return name, validProject(name)
}

// Clone makes a full clone of project (a path or a bare name in Group) at
// <Dir>/<project> on the remote's default branch, and returns its
// directory. The token is passed the same way as for Sync, so it is not
// stored in the clone.
func (m *Manager) Clone(ctx context.Context, project string) (string, error) {
	project, ok := m.ProjectPath(project)
	if !ok {
		return "", ErrInvalidProject
	}
	dest := filepath.Join(m.cfg.Dir, filepath.FromSlash(project))
	defer m.lock(project)()
	if _, err := os.Stat(dest); err == nil {
		return dest, ErrExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return dest, err
	}
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return dest, err
	}
	// Clone next to the destination under a dot name, which List and the
	// rc-session lookup skip, then move it into place.
	tmp, err := os.MkdirTemp(parent, ".clone-"+filepath.Base(dest)+"-")
	if err != nil {
		return dest, err
	}
	defer os.RemoveAll(tmp)
	remote := m.cfg.URL + "/" + project + ".git"
	if _, err := m.gitTimeout(ctx, m.cfg.CloneTimeout, parent, "clone", "--quiet", "--origin", "origin", remote, tmp); err != nil {
		return dest, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return dest, err
	}
	return dest, nil
}

// git runs one git command. Credentials and TLS settings are passed in the
// environment of that process only, so they never land in .git/config, the
// remote URL or the process arguments.
func (m *Manager) git(ctx context.Context, dir string, args ...string) (string, error) {
	return m.gitTimeout(ctx, m.cfg.Timeout, dir, args...)
}

func (m *Manager) gitTimeout(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), m.env()...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("git %s timed out after %s", args[0], timeout)
		}
		// stdout is kept for commands that report through the exit code
		// (merge-tree).
		return stdout.String(), fmt.Errorf("git %s: %w: %s", args[0], err, m.scrub(strings.TrimSpace(stderr.String())))
	}
	return stdout.String(), nil
}

func (m *Manager) env() []string {
	cfg := [][2]string{}
	if m.cfg.Token != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(m.cfg.Username + ":" + m.cfg.Token))
		cfg = append(cfg, [2]string{"http.extraHeader", "Authorization: Basic " + auth})
	}
	if m.cfg.SkipTLSVerify {
		cfg = append(cfg, [2]string{"http.sslVerify", "false"})
	} else if m.cfg.CAFile != "" {
		cfg = append(cfg, [2]string{"http.sslCAInfo", m.cfg.CAFile})
	}
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true", "GIT_CONFIG_NOSYSTEM=1", fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(cfg))}
	for i, kv := range cfg {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, kv[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, kv[1]))
	}
	return env
}

// scrub removes the token from git output before it reaches logs or users.
func (m *Manager) scrub(s string) string {
	if m.cfg.Token != "" {
		s = strings.ReplaceAll(s, m.cfg.Token, "***")
		s = strings.ReplaceAll(s, base64.StdEncoding.EncodeToString([]byte(m.cfg.Username+":"+m.cfg.Token)), "***")
	}
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return redact.Sensitive(s)
}
