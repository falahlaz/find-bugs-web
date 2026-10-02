// Package config loads server settings from environment variables.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config holds every runtime setting.
type Config struct {
	ListenAddr   string
	PublicURL    string
	DBPath       string
	Location     *time.Location
	CookieSecure bool
	SessionTTL   time.Duration
	DevMode      bool

	QueueMax        int
	QueuePerUser    int
	WaitingExpiry   time.Duration
	JobRecheckAfter time.Duration
	JobTimeout      time.Duration
	WatchInterval   time.Duration
	DedupWindow     time.Duration
	RetainRawLogs   time.Duration
	RetainDiagnosis time.Duration

	TransactionIDHeader string

	GP GP

	Splunk Splunk

	Analyzer string // "claude" or "fake"
	Claude   Claude

	TelegramToken  string
	TelegramChatID string
}

// GP configures the GlobalProtect CLI.
type GP struct {
	Bin        string
	Portal     string
	Dir        string
	Browser    string
	ReachHosts []string
}

// Splunk configures the Splunk REST client and SSO re-auth.
type Splunk struct {
	URL               string
	Templates         map[string]string
	SessionPath       string
	SSODomain         string
	SkipTLSVerify     bool
	RequestTimeout    time.Duration // per HTTP request to Splunk
	ResultWaitTimeout time.Duration
	PollInterval      time.Duration
	MaxLogLines       int
	LoginCommand      []string
	LoginTimeout      time.Duration
}

// Environments returns the template keys in stable order.
func (s Splunk) Environments() []string {
	out := make([]string, 0, len(s.Templates))
	for k := range s.Templates {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Claude configures the Claude Code CLI analyzer.
type Claude struct {
	Bin     string
	Model   string
	Timeout time.Duration
}

type loader struct{ errs []string }

func (l *loader) str(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func (l *loader) required(key string) string {
	v := l.str(key, "")
	if v == "" {
		l.errs = append(l.errs, key+" is required")
	}
	return v
}

func (l *loader) int(key string, def int) int {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("%s: %v", key, err))
	}
	return n
}

func (l *loader) bool(key string, def bool) bool {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("%s: %v", key, err))
	}
	return b
}

func (l *loader) dur(key string, def time.Duration) time.Duration {
	v := l.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("%s: %v", key, err))
	}
	return d
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load reads the environment. If full is false only the settings needed by
// CLI subcommands (database, timezone) are validated.
func Load(full bool) (Config, error) {
	l := &loader{}
	home, _ := os.UserHomeDir()
	c := Config{
		ListenAddr:   l.str("LISTEN_ADDR", "127.0.0.1:8080"),
		PublicURL:    strings.TrimRight(l.str("PUBLIC_URL", ""), "/"),
		DBPath:       l.str("DB_PATH", "data/findbugs.db"),
		CookieSecure: l.bool("COOKIE_SECURE", true),
		SessionTTL:   l.dur("SESSION_TTL", 12*time.Hour),
		DevMode:      l.bool("DEV_MODE", false),

		QueueMax:        l.int("QUEUE_MAX", 10),
		QueuePerUser:    l.int("QUEUE_PER_USER", 3),
		WaitingExpiry:   l.dur("WAITING_EXPIRY", 30*time.Minute),
		JobRecheckAfter: l.dur("JOB_RECHECK_AFTER", 5*time.Minute),
		JobTimeout:      l.dur("JOB_TIMEOUT", 15*time.Minute),
		WatchInterval:   l.dur("WATCHDOG_INTERVAL", time.Minute),
		DedupWindow:     l.dur("DEDUP_WINDOW", 24*time.Hour),
		RetainRawLogs:   time.Duration(l.int("RETENTION_RAW_LOG_DAYS", 30)) * 24 * time.Hour,
		RetainDiagnosis: time.Duration(l.int("RETENTION_DIAGNOSIS_DAYS", 180)) * 24 * time.Hour,

		TransactionIDHeader: l.str("TRANSACTION_ID_HEADER", "X-Transaction-ID"),

		Analyzer: l.str("ANALYZER", "claude"),
		Claude: Claude{
			Bin:     l.str("CLAUDE_BIN", "claude"),
			Model:   l.str("CLAUDE_MODEL", "claude-haiku-4-5-20251001"),
			Timeout: l.dur("CLAUDE_TIMEOUT", 5*time.Minute),
		},

		TelegramToken:  l.str("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID: l.str("TELEGRAM_CHAT_ID", ""),
	}
	tz := l.str("TIMEZONE", "Asia/Jakarta")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		l.errs = append(l.errs, fmt.Sprintf("TIMEZONE: %v", err))
		loc = time.UTC
	}
	c.Location = loc

	gpDir := l.str("GP_WEB_DIR", filepath.Join(home, ".gp-web"))
	c.GP = GP{
		Bin:        l.str("GP_BIN", "/usr/bin/globalprotect"),
		Dir:        gpDir,
		Browser:    l.str("BROWSER", filepath.Join(gpDir, "capture-url.sh")),
		ReachHosts: splitList(l.str("GP_REACH_HOSTS", "")),
	}
	c.Splunk = Splunk{
		SessionPath:       l.str("SPLUNK_API_SESSION_PATH", "data/splunk_api_session.json"),
		SkipTLSVerify:     l.bool("SPLUNK_SKIP_SSL_VERIFY", false),
		RequestTimeout:    time.Duration(l.int("SPLUNK_REQUEST_TIMEOUT", 300)) * time.Second,
		ResultWaitTimeout: time.Duration(l.int("SPLUNK_RESULT_WAIT_TIMEOUT", 30)) * time.Second,
		PollInterval:      time.Duration(l.int("SPLUNK_POLL_INTERVAL", 2)) * time.Second,
		MaxLogLines:       l.int("MAX_LOG_LINES", 5000),
		LoginCommand:      strings.Fields(l.str("SPLUNK_LOGIN_CMD", "xvfb-run -a python3 scripts/splunk-login/save_session_auto.py")),
		LoginTimeout:      l.dur("SPLUNK_LOGIN_TIMEOUT", 6*time.Minute),
	}

	if full {
		c.GP.Portal = l.required("GP_PORTAL")
		c.Splunk.URL = strings.TrimRight(l.required("SPLUNK_URL"), "/")
		c.Splunk.SSODomain = l.required("SPLUNK_SSO_DOMAIN")
		raw := l.required("SPLUNK_SPL_TEMPLATES")
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &c.Splunk.Templates); err != nil {
				l.errs = append(l.errs, fmt.Sprintf("SPLUNK_SPL_TEMPLATES: %v", err))
			} else if len(c.Splunk.Templates) == 0 {
				l.errs = append(l.errs, "SPLUNK_SPL_TEMPLATES must have at least one environment")
			}
			for env, tpl := range c.Splunk.Templates {
				if !strings.Contains(tpl, "{transaction_id}") {
					l.errs = append(l.errs, fmt.Sprintf("SPLUNK_SPL_TEMPLATES[%s] has no {transaction_id}", env))
				}
			}
		}
		if c.Analyzer != "claude" && c.Analyzer != "fake" {
			l.errs = append(l.errs, "ANALYZER must be claude or fake")
		}
		if c.QueueMax < 1 || c.QueuePerUser < 1 {
			l.errs = append(l.errs, "QUEUE_MAX and QUEUE_PER_USER must be >= 1")
		}
	}
	if len(l.errs) > 0 {
		return c, fmt.Errorf("config: %s", strings.Join(l.errs, "; "))
	}
	return c, nil
}
