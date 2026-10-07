// Package usage reports the Claude subscription limits the analyzer draws
// on: the 5-hour and weekly windows shown on claude.ai. The CLI reports them
// in a rate_limit_event on --output-format stream-json, so a probe is a tiny
// one-turn Haiku prompt (a few hundred tokens).
package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Window is one rate limit window.
type Window struct {
	// Percent of the window used, 0-100.
	Percent  float64   `json:"percent" doc:"Percent of the window used, 0-100"`
	ResetsAt time.Time `json:"resetsAt"`
}

// Snapshot is the last probe result.
type Snapshot struct {
	FiveHour  *Window   `json:"fiveHour"`
	SevenDay  *Window   `json:"sevenDay"`
	Status    string    `json:"status" doc:"allowed, allowed_warning or rejected"`
	CheckedAt time.Time `json:"checkedAt"`
}

// Prober runs and caches the probe.
type Prober struct {
	Bin     string
	Timeout time.Duration
	// MaxAge is how long a snapshot is served before a GET re-probes.
	MaxAge time.Duration
	// MinGap is the shortest gap between probes, even when forced.
	MinGap time.Duration

	mu   sync.Mutex
	last *Snapshot
	run  func(ctx context.Context) ([]byte, error) // for tests
}

// Get returns the cached snapshot, probing first when it is older than
// MaxAge, or older than MinGap when force is set.
func (p *Prober) Get(ctx context.Context, force bool) (Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last != nil {
		age := time.Since(p.last.CheckedAt)
		if age < p.MinGap || (!force && age < p.MaxAge) {
			return *p.last, nil
		}
	}
	out, err := p.exec(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	s, err := Parse(out)
	if err != nil {
		return Snapshot{}, err
	}
	s.CheckedAt = time.Now()
	p.last = &s
	return s, nil
}

func (p *Prober) exec(ctx context.Context) ([]byte, error) {
	if p.run != nil {
		return p.run(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "fbw-usage-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, p.Bin,
		"-p",
		"--model", "haiku",
		"--output-format", "stream-json", "--verbose",
		"--tools", "",
		"--strict-mcp-config",
		"--setting-sources", "",
		"--no-session-persistence",
		"--max-turns", "1",
		"--system-prompt", "Reply with: ok",
	)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("ok")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude timed out after %s", p.Timeout)
		}
		// A rejected (limit reached) run can still carry the event.
		if bytes.Contains(stdout.Bytes(), []byte(`"rate_limit_event"`)) {
			return stdout.Bytes(), nil
		}
		return nil, fmt.Errorf("claude failed: %w: %s", err, tail(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Parse reads the last rate_limit_event from stream-json output.
func Parse(out []byte) (Snapshot, error) {
	type window struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    int64   `json:"resetsAt"`
	}
	var found *Snapshot
	for _, line := range bytes.Split(out, []byte("\n")) {
		if !bytes.Contains(line, []byte(`"rate_limit_event"`)) {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Info struct {
				Status  string             `json:"status"`
				Windows map[string]*window `json:"unifiedWindows"`
			} `json:"rate_limit_info"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type != "rate_limit_event" {
			continue
		}
		conv := func(w *window) *Window {
			if w == nil {
				return nil
			}
			return &Window{Percent: w.Utilization * 100, ResetsAt: time.Unix(w.ResetsAt, 0).UTC()}
		}
		found = &Snapshot{
			Status:   ev.Info.Status,
			FiveHour: conv(ev.Info.Windows["five_hour"]),
			SevenDay: conv(ev.Info.Windows["seven_day"]),
		}
	}
	if found == nil {
		return Snapshot{}, errors.New("claude output has no rate_limit_event (is the CLI logged in with a Claude subscription?)")
	}
	return *found, nil
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = "…" + s[len(s)-300:]
	}
	return s
}
