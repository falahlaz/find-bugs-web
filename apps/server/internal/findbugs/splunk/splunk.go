// Package splunk is a Splunk REST client that reuses the browser SSO session
// cookies saved by scripts/splunk-login (ported from find-bugs-bot
// scraper/splunk_api.py and splunk_scraper.py).
package splunk

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/redact"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/config"
)

const apiPrefix = "/en-US/splunkd/__raw"

// RequiredCookies are written by the login script.
var RequiredCookies = []string{"splunkd_8008", "session_id_8008", "splunkweb_csrf_token_8008", "token_key"}

// Errors.
var (
	ErrSessionExpired = errors.New("splunk session expired")
	ErrNoSession      = errors.New("splunk session file missing or invalid")
	ErrReauthBusy     = errors.New("splunk re-auth already running")
	ErrUnknownEnv     = errors.New("unknown environment")
)

// Search outcomes.
const (
	ResultSuccess = "success"
	ResultNoLogs  = "no_logs"
)

// Result is a finished search.
type Result struct {
	Status string
	// Logs is Events rendered as "[_time] _raw" lines, oldest first.
	Logs string
	// Events are the fetched events, oldest first.
	Events []Event
	// EventCount is how many events the search matched; Truncated is set
	// when that is more than MaxLogLines and only the newest were fetched.
	EventCount int
	Truncated  bool
}

// Event is one Splunk event.
type Event struct {
	Time string
	Raw  string
}

// eventsPageSize is how many events are requested per /events call.
const eventsPageSize = 1000

type sessionFile struct {
	BaseURL   string            `json:"base_url"`
	Cookies   map[string]string `json:"cookies"`
	CSRFToken string            `json:"csrf_token"`
}

// SessionInfo describes the loaded session for the status panel.
type SessionInfo struct {
	Loaded        bool       `json:"loaded"`
	SavedAt       *time.Time `json:"savedAt,omitempty"`
	Missing       []string   `json:"missingCookies,omitempty"`
	ReauthRunning bool       `json:"reauthRunning"`
	LastReauthAt  *time.Time `json:"lastReauthAt,omitempty"`
	LastReauthOK  *bool      `json:"lastReauthOk,omitempty"`
	LastReauthLog []string   `json:"lastReauthLog,omitempty"`
}

// Client talks to Splunk. It is safe for concurrent use.
type Client struct {
	cfg  config.Splunk
	http *http.Client

	mu      sync.RWMutex
	cookies string
	csrf    string
	info    SessionInfo

	reauthMu sync.Mutex
}

// New returns a client; call Load before searching.
func New(cfg config.Splunk) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.SkipTLSVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in for corporate MITM proxies
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Transport:     tr,
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Load (re)reads the session file.
func (c *Client) Load() error {
	b, err := os.ReadFile(c.cfg.SessionPath)
	st, statErr := os.Stat(c.cfg.SessionPath)
	var sf sessionFile
	if err == nil {
		err = json.Unmarshal(b, &sf)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil || len(sf.Cookies) == 0 {
		c.cookies, c.csrf = "", ""
		c.info.Loaded, c.info.SavedAt, c.info.Missing = false, nil, nil
		if err == nil {
			err = errors.New("no cookies")
		}
		return fmt.Errorf("%w: %v", ErrNoSession, err)
	}
	names := make([]string, 0, len(sf.Cookies))
	for k := range sf.Cookies {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, k+"="+sf.Cookies[k])
	}
	var missing []string
	for _, k := range RequiredCookies {
		if _, ok := sf.Cookies[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		slog.Warn("splunk session missing cookies", "missing", missing)
	}
	csrf := sf.CSRFToken
	if csrf == "" {
		csrf = sf.Cookies["splunkweb_csrf_token_8008"]
	}
	c.cookies, c.csrf = strings.Join(parts, "; "), csrf
	c.info.Loaded, c.info.Missing = true, missing
	if statErr == nil {
		t := st.ModTime()
		c.info.SavedAt = &t
	}
	return nil
}

// Info returns the session state.
func (c *Client) Info() SessionInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	info := c.info
	info.LastReauthLog = append([]string(nil), c.info.LastReauthLog...)
	return info
}

func (c *Client) do(ctx context.Context, method, path string, form url.Values, query url.Values) (*http.Response, error) {
	c.mu.RLock()
	cookies, csrf := c.cookies, c.csrf
	c.mu.RUnlock()
	if cookies == "" {
		return nil, ErrNoSession
	}
	u := c.cfg.URL + apiPrefix + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookies)
	req.Header.Set("X-Splunk-Form-Key", csrf)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("splunk request: %w", err)
	}
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		loc := resp.Header.Get("Location")
		resp.Body.Close()
		if c.cfg.SSODomain != "" && strings.Contains(strings.ToLower(loc), strings.ToLower(c.cfg.SSODomain)) {
			return nil, fmt.Errorf("%w: redirected to SSO", ErrSessionExpired)
		}
		if strings.Contains(strings.ToLower(loc), "login") {
			return nil, fmt.Errorf("%w: redirected to login", ErrSessionExpired)
		}
		return nil, fmt.Errorf("splunk unexpected redirect (HTTP %d)", resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		resp.Body.Close()
		return nil, fmt.Errorf("%w: HTTP %d", ErrSessionExpired, resp.StatusCode)
	}
	return resp, nil
}

func readJSON(resp *http.Response, ok []int, v any) error {
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	good := false
	for _, code := range ok {
		good = good || resp.StatusCode == code
	}
	if !good {
		return fmt.Errorf("splunk HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("splunk invalid JSON: %s", snippet(b))
	}
	return nil
}

func snippet(b []byte) string {
	s := string(bytes.TrimSpace(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return redact.Sensitive(s)
}

// BuildQuery renders the SPL template for env (Python str.format semantics
// for {transaction_id}, {{ and }}) and appends " NOT kong". The transaction ID
// must already be validated by the parser.
func (c *Client) BuildQuery(env, txn string) (string, error) {
	tpl, ok := c.cfg.Templates[env]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownEnv, env)
	}
	q := strings.ReplaceAll(tpl, "{transaction_id}", "\x00")
	q = strings.NewReplacer("{{", "{", "}}", "}").Replace(q)
	q = strings.ReplaceAll(q, "\x00", txn)
	return q + " NOT kong", nil
}

// Search runs the query for env/txn over the last timeRange (e.g. "24h").
func (c *Client) Search(ctx context.Context, env, txn, timeRange string) (Result, error) {
	q, err := c.BuildQuery(env, txn)
	if err != nil {
		return Result{}, err
	}
	sid, err := c.createJob(ctx, q, timeRange)
	if err != nil {
		return Result{}, err
	}
	defer c.cleanup(sid)
	count, err := c.poll(ctx, sid)
	if err != nil {
		return Result{}, err
	}
	if count == 0 {
		return Result{Status: ResultNoLogs}, nil
	}
	max := c.cfg.MaxLogLines
	if max <= 0 {
		max = 5000
	}
	events, err := c.events(ctx, sid, min(count, max))
	if err != nil {
		return Result{}, err
	}
	lines := make([]string, 0, len(events))
	for _, ev := range events {
		if ev.Time != "" {
			lines = append(lines, fmt.Sprintf("[%s] %s", ev.Time, ev.Raw))
		} else {
			lines = append(lines, ev.Raw)
		}
	}
	logs := strings.Join(lines, "\n")
	if strings.TrimSpace(logs) == "" {
		return Result{Status: ResultNoLogs}, nil
	}
	return Result{Status: ResultSuccess, Logs: logs, Events: events, EventCount: count, Truncated: count > max}, nil
}

func (c *Client) createJob(ctx context.Context, q, timeRange string) (string, error) {
	form := url.Values{
		"search":         {"search " + q},
		"earliest_time":  {"-" + timeRange},
		"latest_time":    {"now"},
		"output_mode":    {"json"},
		"rf":             {"*"},
		"status_buckets": {"300"},
		"auto_cancel":    {"120"},
	}
	resp, err := c.do(ctx, http.MethodPost, "/services/search/v2/jobs", form, nil)
	if err != nil {
		return "", err
	}
	var out struct {
		SID string `json:"sid"`
	}
	if err := readJSON(resp, []int{200, 201}, &out); err != nil {
		return "", fmt.Errorf("create job: %w", err)
	}
	if out.SID == "" {
		return "", errors.New("create job: no sid in response")
	}
	return out.SID, nil
}

func (c *Client) poll(ctx context.Context, sid string) (int, error) {
	deadline := time.Now().Add(c.cfg.ResultWaitTimeout)
	interval := c.cfg.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for {
		resp, err := c.do(ctx, http.MethodGet, "/services/search/v2/jobs/"+url.PathEscape(sid), nil, url.Values{"output_mode": {"json"}})
		if err != nil {
			return 0, err
		}
		var out struct {
			Entry []struct {
				Content struct {
					IsDone        bool   `json:"isDone"`
					DispatchState string `json:"dispatchState"`
					EventCount    int    `json:"eventCount"`
				} `json:"content"`
			} `json:"entry"`
		}
		if err := readJSON(resp, []int{200}, &out); err != nil {
			return 0, fmt.Errorf("poll job: %w", err)
		}
		if len(out.Entry) == 0 {
			return 0, errors.New("poll job: no entry in response")
		}
		ct := out.Entry[0].Content
		if ct.IsDone {
			switch ct.DispatchState {
			case "FAILED", "INTERNAL_CANCEL", "BAD_INPUT_CANCEL", "QUIT":
				return 0, fmt.Errorf("search job ended with state %s", ct.DispatchState)
			}
			return ct.EventCount, nil
		}
		if time.Now().Add(interval).After(deadline) {
			return 0, fmt.Errorf("search job timed out after %s", c.cfg.ResultWaitTimeout)
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// field accepts either "x" or {"value": "x"} (Splunk returns both shapes).
type field string

func (f *field) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*f = field(s)
		return nil
	}
	var o struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(b, &o) == nil {
		*f = field(o.Value)
	}
	return nil
}

// events fetches up to want events page by page. Splunk returns the newest
// first; the result is reversed so it reads oldest first.
func (c *Client) events(ctx context.Context, sid string, want int) ([]Event, error) {
	var events []Event
	for offset := 0; offset < want; offset += eventsPageSize {
		q := url.Values{
			"output_mode":  {"json"},
			"offset":       {fmt.Sprint(offset)},
			"count":        {fmt.Sprint(min(eventsPageSize, want-offset))},
			"field_list":   {"_raw,_time,source,sourcetype,host"},
			"max_lines":    {"0"},
			"segmentation": {"none"},
		}
		resp, err := c.do(ctx, http.MethodGet, "/services/search/v2/jobs/"+url.PathEscape(sid)+"/events", nil, q)
		if err != nil {
			return nil, err
		}
		var out struct {
			Results []struct {
				Raw  field `json:"_raw"`
				Time field `json:"_time"`
			} `json:"results"`
		}
		if err := readJSON(resp, []int{200}, &out); err != nil {
			return nil, fmt.Errorf("get events: %w", err)
		}
		for _, ev := range out.Results {
			if ev.Raw == "" {
				continue
			}
			t := string(ev.Time)
			if t == "0" {
				t = ""
			}
			events = append(events, Event{Time: t, Raw: string(ev.Raw)})
		}
		if len(out.Results) < min(eventsPageSize, want-offset) {
			break
		}
	}
	slices.Reverse(events)
	return events, nil
}

func (c *Client) cleanup(sid string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.do(ctx, http.MethodPost, "/services/search/v2/jobs/"+url.PathEscape(sid)+"/control",
		url.Values{"action": {"cancel"}, "output_mode": {"json"}}, nil)
	if err != nil {
		slog.Debug("splunk job cleanup failed", "sid", sid, "err", err)
		return
	}
	resp.Body.Close()
}

// Check verifies the session is still accepted with a cheap request.
func (c *Client) Check(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/services/authentication/current-context", nil, url.Values{"output_mode": {"json"}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("splunk check HTTP %d", resp.StatusCode)
	}
	return nil
}

// ReAuth runs the SSO login script (which waits for the 2FA push) and reloads
// the session. Only one run at a time; a second caller gets ErrReauthBusy.
func (c *Client) ReAuth(ctx context.Context) error {
	if !c.reauthMu.TryLock() {
		return ErrReauthBusy
	}
	defer c.reauthMu.Unlock()
	if len(c.cfg.LoginCommand) == 0 {
		return errors.New("SPLUNK_LOGIN_CMD is empty")
	}
	c.mu.Lock()
	c.info.ReauthRunning = true
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, c.cfg.LoginTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.cfg.LoginCommand[0], c.cfg.LoginCommand[1:]...)
	cmd.Env = append(os.Environ(), "SPLUNK_API_SESSION_PATH="+c.cfg.SessionPath, "SPLUNK_URL="+c.cfg.URL)
	// Own process group so a timeout also kills Xvfb and Chromium.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	if runErr == nil {
		runErr = c.Load()
	}
	ok := runErr == nil
	now := time.Now()
	c.mu.Lock()
	c.info.ReauthRunning = false
	c.info.LastReauthAt, c.info.LastReauthOK = &now, &ok
	c.info.LastReauthLog = cleanOutput(out.String())
	c.mu.Unlock()
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("splunk login timed out after %s (2FA not approved?)", c.cfg.LoginTimeout)
		}
		return fmt.Errorf("splunk login failed: %w", runErr)
	}
	return nil
}

// cleanOutput keeps the last 40 lines of script output with secrets removed.
func cleanOutput(s string) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), "csrf") || strings.Contains(strings.ToLower(l), "cookie") {
			l = "[line hidden: may contain session data]"
		}
		out = append(out, redact.Sensitive(l))
	}
	return out
}
