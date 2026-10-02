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
var podLogPath = regexp.MustCompile(`^/var/log/pods/[^/_]+_[^/_]+_[^/]+/([a-z0-9]([a-z0-9-]*[a-z0-9])?)/[0-9]+\.log$`)

// Container returns the Kubernetes container name in a Splunk source path,
// or "" if source is not a pod log path.
func Container(source string) string {
	m := podLogPath.FindStringSubmatch(source)
	if m == nil {
		return ""
	}
	return m[1]
}

// Containers lists the containers that logged events, most events first
// (ties by name).
func Containers(events []splunk.Event) []string {
	n := map[string]int{}
	for _, ev := range events {
		if c := Container(ev.Source); c != "" {
			n[c]++
		}
	}
	out := make([]string, 0, len(n))
	for c := range n {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if n[out[i]] != n[out[j]] {
			return n[out[i]] > n[out[j]]
		}
		return out[i] < out[j]
	})
	return out
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
	Dir           string            // where checkouts live: <Dir>/<group>/<project>
	Group         string            // default group for a container
	RepoMap       map[string]string // container -> group/project overrides
	Ref           string            // branch to check out
	Timeout       time.Duration     // per git command
}

// Checkout is a repo synced to Ref.
type Checkout struct {
	Container string
	Project   string // group/project
	Dir       string
	Ref       string
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

// Sync clones the container's repo if needed, or fetches it, and leaves a
// clean worktree at the tip of Ref. Only the latest commit is kept.
func (m *Manager) Sync(ctx context.Context, container string) (Checkout, error) {
	project := m.Project(container)
	co := Checkout{Container: container, Project: project, Ref: m.cfg.Ref}
	if !validProject(project) {
		return co, fmt.Errorf("invalid GitLab project %q", project)
	}
	co.Dir = filepath.Join(m.cfg.Dir, filepath.FromSlash(project))
	defer m.lock(project)()

	remote := m.cfg.URL + "/" + project + ".git"
	if _, err := os.Stat(filepath.Join(co.Dir, ".git")); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(co.Dir), 0o755); err != nil {
			return co, err
		}
		// A leftover directory from an interrupted clone would block git.
		_ = os.RemoveAll(co.Dir)
		if _, err := m.git(ctx, "", "clone", "--quiet", "--depth", "1", "--single-branch", "--no-tags", "--branch", m.cfg.Ref, remote, co.Dir); err != nil {
			_ = os.RemoveAll(co.Dir)
			return co, err
		}
	} else {
		if _, err := m.git(ctx, co.Dir, "fetch", "--quiet", "--depth", "1", "--no-tags", remote, m.cfg.Ref); err != nil {
			return co, err
		}
		if _, err := m.git(ctx, co.Dir, "reset", "--quiet", "--hard", "FETCH_HEAD"); err != nil {
			return co, err
		}
		if _, err := m.git(ctx, co.Dir, "clean", "-qfdx"); err != nil {
			return co, err
		}
	}
	out, err := m.git(ctx, co.Dir, "rev-parse", "HEAD")
	if err != nil {
		return co, err
	}
	co.Commit = strings.TrimSpace(out)
	return co, nil
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
