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
	// RetainLogFiles keeps the per-job log files (data/logs) this long.
	RetainLogFiles time.Duration
	// KnowledgeDir is the trace-report knowledge base shown on the Laporan
	// page (<dir>/<repo>/<date>-<slug>/report.{md,html}).
	KnowledgeDir string
	// TselCiphersFile and TselPrivateKey are the MyTelkomsel tools' secrets
	// (cipher passwords JSON, and the services' private.pem).
	TselCiphersFile string
	TselPrivateKey  string
	// MRTriageAuthor is the name tag "| <author> |" in the MR titles the MR
	// Triage page lists.
	MRTriageAuthor string

	TransactionIDHeader string

	GP GP

	Splunk Splunk

	Analyzer string // "claude" or "fake"
	Claude   Claude

	GitLab GitLab

	RCSession RCSession

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
	// CorrelationMaxIDs caps how many backend IDs linked to the searched
	// transaction ID are re-searched; 0 disables it.
	CorrelationMaxIDs int
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

// GitLab configures code tracing: cloning service repos from GitLab so the
// analyzer can point an internal error at a file and line.
type GitLab struct {
	URL           string
	Username      string
	Token         string
	CAFile        string
	SkipTLSVerify bool
	ReposDir      string
	// Group is where a service's repo lives when RepoMap has no entry for its
	// Kubernetes container name: <Group>/<container>.
	Group   string
	RepoMap map[string]string // container name -> group/project
	Ref     string
	Timeout time.Duration // per git command
	// CloneTimeout bounds a full clone made from the Repo page.
	CloneTimeout time.Duration
	// MaxRepos caps how many repos one job traces into.
	MaxRepos int
	// Model and TraceTimeout are for the code-tracing pass, which needs a
	// stronger model than the log diagnosis.
	Model        string
	TraceTimeout time.Duration
	// TraceEnabled is CODE_TRACE_ENABLED, the switch to turn tracing off
	// without removing the GitLab settings.
	TraceEnabled bool
	// EnvMap maps a Kubernetes namespace (from the Splunk source path) to the
	// GitLab environment whose last deploy-eks:<env> job says which commit
	// runs there. Namespaces not in it are traced on Ref.
	EnvMap map[string]string
	// WorktreeTTL is how long a per-commit checkout no trace session needs
	// is kept after its last use; 0 keeps them forever.
	WorktreeTTL time.Duration
	// SessionIdle closes a trace chat after this long without a message; it
	// can be reopened. SessionRetention deletes the Claude session files of
	// chats idle this long (the chat history stays readable).
	SessionIdle      time.Duration
	SessionRetention time.Duration
	// ChatConcurrency caps the chat and re-trace turns running at once.
	ChatConcurrency int
	// ConfigProject is the repo the servers' JSON config (ConfigMaps) is
	// built from, one branch per environment; "" leaves it out of traces.
	// ConfigPath is the directory of the JSON files in it, ConfigBranches
	// maps a GitLab environment to its branch (the same name when absent)
	// and ConfigDeployJob starts the names of the jobs deploying a branch.
	ConfigProject   string
	ConfigPath      string
	ConfigBranches  map[string]string
	ConfigDeployJob string
}

// Environments lists the GitLab environments of an env map once each, the
// usual promotion order first.
func Environments(envMap map[string]string) []string {
	rank := map[string]int{"dev": 1, "staging": 2, "preprod": 3, "blue": 4, "production": 5}
	seen := map[string]bool{}
	var out []string
	for _, e := range envMap {
		if e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := rank[out[i]], rank[out[j]]
		if ri == 0 {
			ri = 99
		}
		if rj == 0 {
			rj = 99
		}
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

// RCSession configures starting Claude Remote Control sessions in the repos
// under GitLab.ReposDir from the website, through the rc-session skill's
// script.
type RCSession struct {
	Script string
	// Launcher prefixes the script command; nil runs it directly.
	Launcher []string
}

// DefaultEnvMap is GITLAB_ENV_MAP when unset.
var DefaultEnvMap = map[string]string{
	"tdw-dev": "dev", "tdw-staging": "staging", "tdw-preprod": "preprod", "blue": "blue", "tdw-webapi": "production",
}

// Enabled reports whether code tracing is switched on and configured.
func (g GitLab) Enabled() bool { return g.TraceEnabled && g.URL != "" && g.Token != "" }

// CanClone reports whether repos can be cloned from GitLab (also with
// tracing switched off).
func (g GitLab) CanClone() bool { return g.URL != "" && g.Token != "" }

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
		RetainRawLogs:   time.Duration(l.int("RETENTION_RAW_LOG_DAYS", 7)) * 24 * time.Hour,
		RetainDiagnosis: time.Duration(l.int("RETENTION_DIAGNOSIS_DAYS", 180)) * 24 * time.Hour,
		RetainLogFiles:  time.Duration(l.int("RETENTION_LOG_FILE_DAYS", 3)) * 24 * time.Hour,
		KnowledgeDir:    l.str("KNOWLEDGE_DIR", filepath.Join(home, "knowledge")),
		TselCiphersFile: l.str("TSEL_CIPHERS_FILE", filepath.Join(home, ".config", "findbugs", "tsel-ciphers.json")),
		TselPrivateKey:  l.str("TSEL_PRIVATE_KEY_PATH", filepath.Join(home, ".config", "findbugs", "tsel-private.pem")),
		MRTriageAuthor:  l.str("MR_TRIAGE_AUTHOR", "Falah"),

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
		CorrelationMaxIDs: l.int("SPLUNK_CORRELATION_MAX_IDS", 5),
		LoginCommand:      strings.Fields(l.str("SPLUNK_LOGIN_CMD", "xvfb-run -a python3 scripts/splunk-login/save_session_auto.py")),
		LoginTimeout:      l.dur("SPLUNK_LOGIN_TIMEOUT", 6*time.Minute),
	}

	c.GitLab = GitLab{
		URL:           strings.TrimRight(l.str("GITLAB_URL", ""), "/"),
		Username:      l.str("GITLAB_USERNAME", "oauth2"),
		Token:         l.str("GITLAB_TOKEN", ""),
		CAFile:        l.str("GITLAB_CA_FILE", ""),
		SkipTLSVerify: l.bool("GITLAB_SKIP_SSL_VERIFY", false),
		ReposDir:      l.str("GITLAB_REPOS_DIR", filepath.Join(home, "gitlab-services")),
		Group:         l.str("GITLAB_GROUP", "my-telkomsel"),
		Ref:           l.str("GITLAB_REF", "main"),
		Timeout:       l.dur("GITLAB_GIT_TIMEOUT", 3*time.Minute),
		CloneTimeout:  l.dur("GITLAB_CLONE_TIMEOUT", 15*time.Minute),
		MaxRepos:      l.int("CODE_TRACE_MAX_REPOS", 3),
		Model:         l.str("CODE_TRACE_MODEL", "claude-opus-5-5"),
		TraceTimeout:  l.dur("CODE_TRACE_TIMEOUT", 8*time.Minute),
		TraceEnabled:  l.bool("CODE_TRACE_ENABLED", true),
		EnvMap:        DefaultEnvMap,

		WorktreeTTL:      time.Duration(l.int("GITLAB_WORKTREE_TTL_DAYS", 14)) * 24 * time.Hour,
		SessionIdle:      l.dur("TRACE_SESSION_IDLE", 10*time.Minute),
		SessionRetention: time.Duration(l.int("TRACE_SESSION_RETENTION_DAYS", 30)) * 24 * time.Hour,
		ChatConcurrency:  l.int("TRACE_CHAT_CONCURRENCY", 2),

		ConfigProject:   l.str("GITLAB_CONFIG_PROJECT", "my-telkomsel/devsecops/json-config-updater"),
		ConfigPath:      strings.Trim(l.str("GITLAB_CONFIG_PATH", "json-files"), "/"),
		ConfigDeployJob: l.str("GITLAB_CONFIG_DEPLOY_JOB", "deploy_configmaps"),
	}
	if os.Getenv("GITLAB_CONFIG_PROJECT") == "-" {
		c.GitLab.ConfigProject = ""
	}
	if raw := l.str("GITLAB_CONFIG_BRANCH_MAP", ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c.GitLab.ConfigBranches); err != nil {
			l.errs = append(l.errs, fmt.Sprintf("GITLAB_CONFIG_BRANCH_MAP: %v", err))
		}
	}
	c.RCSession = RCSession{
		Script:   l.str("RC_SESSION_SCRIPT", filepath.Join(home, ".claude", "skills", "rc-session", "rc-session.sh")),
		Launcher: strings.Fields(l.str("RC_SESSION_LAUNCHER", "systemd-run --user --scope --quiet --collect --")),
	}
	if os.Getenv("RC_SESSION_LAUNCHER") == "-" {
		c.RCSession.Launcher = nil
	}
	if raw := l.str("GITLAB_ENV_MAP", ""); raw != "" {
		c.GitLab.EnvMap = nil
		if err := json.Unmarshal([]byte(raw), &c.GitLab.EnvMap); err != nil {
			l.errs = append(l.errs, fmt.Sprintf("GITLAB_ENV_MAP: %v", err))
		}
	}
	if c.GitLab.ChatConcurrency < 1 {
		c.GitLab.ChatConcurrency = 1
	}
	if raw := l.str("GITLAB_REPO_MAP", ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c.GitLab.RepoMap); err != nil {
			l.errs = append(l.errs, fmt.Sprintf("GITLAB_REPO_MAP: %v", err))
		}
	}
	// With TLS checks off the CA file is not used, so a placeholder path that
	// does not exist yet must not break tracing.
	if c.GitLab.SkipTLSVerify {
		c.GitLab.CAFile = ""
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
		if c.GitLab.Enabled() && c.GitLab.CAFile != "" {
			if _, err := os.Stat(c.GitLab.CAFile); err != nil {
				l.errs = append(l.errs, fmt.Sprintf("GITLAB_CA_FILE: %v", err))
			}
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
