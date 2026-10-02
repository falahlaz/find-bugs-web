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
var podLogPath = regexp.MustCompile(`^/var/log/pods/([a-z0-9]([a-z0-9-]*[a-z0-9])?)_[^/_]+_[^/]+/([a-z0-9]([a-z0-9-]*[a-z0-9])?)/[0-9]+\.log$`)

// Container returns the Kubernetes container name in a Splunk source path,
// or "" if source is not a pod log path.
func Container(source string) string {
	if m := podLogPath.FindStringSubmatch(source); m != nil {
		return m[3]
	}
	return ""
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
		if _, err := m.git(ctx, store, "fetch", "--quiet", "--depth", "1", "--no-tags", remote, ref); err != nil {
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

// git runs one git command. Credentials and TLS settings are passed in the
// environment of that process only, so they never land in .git/config, the
// remote URL or the process arguments.
func (m *Manager) git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, m.cfg.Timeout)
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
			return "", fmt.Errorf("git %s timed out after %s", args[0], m.cfg.Timeout)
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, m.scrub(strings.TrimSpace(stderr.String())))
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
