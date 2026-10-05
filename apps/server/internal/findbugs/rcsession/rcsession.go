// Package rcsession starts and stops Claude Remote Control (`claude rc`)
// sessions for the repos under the repos directory. The work is done by the
// rc-session skill's script, so a session started from the website is the
// same as one started with /rc-session: a tmux session named after the
// folder and tagged with the tmux option @rc_dir.
package rcsession

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/redact"
)

// Config configures a Manager.
type Config struct {
	Script    string // rc-session.sh
	ClaudeBin string // its directory is put on PATH for the script
	Root      string // only folders under it get sessions
	// Launcher prefixes the script command. In production it is
	// `systemd-run --user --scope ... --`, so a tmux server the script
	// starts is not in the service's cgroup and survives a restart of the
	// website. Empty runs the script directly.
	Launcher []string
	Tmux     string // tmux binary, default "tmux"
}

// Session is a running rc session.
type Session struct {
	Name  string
	Dir   string
	URL   string // claude.ai link, "" until it connected
	Since time.Time
}

// ErrOutsideRoot is returned for a folder outside Config.Root.
var ErrOutsideRoot = errors.New("folder di luar direktori repo")

// Manager runs the script. Start and Stop for one folder are serialised.
type Manager struct {
	cfg   Config
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New returns a Manager.
func New(cfg Config) *Manager {
	if cfg.Tmux == "" {
		cfg.Tmux = "tmux"
	}
	return &Manager{cfg: cfg, locks: map[string]*sync.Mutex{}}
}

// Enabled reports whether the script exists.
func (m *Manager) Enabled() bool {
	if m == nil || m.cfg.Script == "" {
		return false
	}
	fi, err := os.Stat(m.cfg.Script)
	return err == nil && !fi.IsDir()
}

var urlRe = regexp.MustCompile(`https://claude\.ai/code\?environment=[A-Za-z0-9_]+`)

// Sessions returns the sessions started by the script, by folder. No tmux
// server running means no sessions.
func (m *Manager) Sessions(ctx context.Context) (map[string]Session, error) {
	out, err := m.run(ctx, 10*time.Second, m.cfg.Tmux, "ls", "-F", "#{session_name}|#{@rc_dir}|#{session_created}")
	if err != nil {
		// tmux exits 1 with "no server running" (or "error connecting")
		// when there is nothing to list.
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return map[string]Session{}, nil
		}
		return nil, err
	}
	sessions := map[string]Session{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "|", 3)
		if len(f) != 3 || f[1] == "" {
			continue
		}
		s := Session{Name: f[0], Dir: f[1]}
		if sec, err := strconv.ParseInt(f[2], 10, 64); err == nil {
			s.Since = time.Unix(sec, 0)
		}
		if pane, err := m.run(ctx, 5*time.Second, m.cfg.Tmux, "capture-pane", "-J", "-p", "-t", "="+s.Name+":"); err == nil {
			s.URL = urlRe.FindString(pane)
		}
		sessions[s.Dir] = s
	}
	return sessions, nil
}

// Name is the tmux session name the script uses for dir.
func Name(dir string) string {
	return strings.NewReplacer(".", "-", ":", "-").Replace(filepath.Base(dir))
}

// Start starts a session for dir and returns the script's report.
func (m *Manager) Start(ctx context.Context, dir string) (string, error) {
	dir, err := m.check(dir)
	if err != nil {
		return "", err
	}
	defer m.lock(dir)()
	// The script names sessions after the folder only, and treats a
	// session of that name as this folder's.
	if all, err := m.Sessions(ctx); err == nil {
		for _, s := range all {
			if s.Name == Name(dir) && s.Dir != dir {
				return "", fmt.Errorf("sesi %q sudah dipakai folder lain (%s)", s.Name, s.Dir)
			}
		}
	}
	// Trust prompt (up to ~25s) plus waiting for Connected (30s).
	return m.script(ctx, 90*time.Second, "start", dir)
}

// Stop kills dir's session and returns the script's report.
func (m *Manager) Stop(ctx context.Context, dir string) (string, error) {
	dir, err := m.check(dir)
	if err != nil {
		return "", err
	}
	defer m.lock(dir)()
	return m.script(ctx, 30*time.Second, "stop", dir)
}

// check cleans dir and makes sure it is a folder under Root. The script
// gets the absolute path, so it never searches by name.
func (m *Manager) check(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", ErrOutsideRoot
	}
	dir = filepath.Clean(dir)
	rel, err := filepath.Rel(filepath.Clean(m.cfg.Root), dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrOutsideRoot
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("folder %s tidak ada", dir)
	}
	return dir, nil
}

func (m *Manager) lock(dir string) func() {
	m.mu.Lock()
	l := m.locks[dir]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[dir] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (m *Manager) script(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	cmd := append(append(append([]string{}, m.cfg.Launcher...), m.cfg.Script), args...)
	out, err := m.run(ctx, timeout, cmd[0], cmd[1:]...)
	out = strings.TrimSpace(redact.Sensitive(out))
	if err != nil {
		if out == "" {
			out = err.Error()
		}
		return out, errors.New(out)
	}
	return out, nil
}

// run runs a command with a minimal environment: the service's own
// environment holds the GitLab token and other secrets, which must not
// reach tmux or the Claude sessions started in it.
func (m *Manager) run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = m.cfg.Root
	cmd.Env = m.env()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return buf.String(), fmt.Errorf("%s timed out after %s", filepath.Base(name), timeout)
	}
	return buf.String(), err
}

func (m *Manager) env() []string {
	path := "/usr/local/bin:/usr/bin:/bin"
	if m.cfg.ClaudeBin != "" && filepath.IsAbs(m.cfg.ClaudeBin) {
		path = filepath.Dir(m.cfg.ClaudeBin) + ":" + path
	} else if home, err := os.UserHomeDir(); err == nil {
		path = filepath.Join(home, ".local", "bin") + ":" + path
	}
	env := []string{"PATH=" + path, "TERM=xterm-256color", "RC_SESSION_ROOT=" + m.cfg.Root}
	for _, k := range []string{"HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "TMUX_TMPDIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}
