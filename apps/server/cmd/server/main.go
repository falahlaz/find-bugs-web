// Command server runs Find Bugs Web.
//
//	server [serve]                         run the web server and worker
//	server user create <username> <role>  create an account (password from stdin or prompt)
//	server user passwd <username>          reset a password
//	server openapi                         print the OpenAPI document
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/api"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/analyzer"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/configrepo"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/jobs"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/mrtriage"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/rcsession"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/tracechat"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/usage"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/watchdog"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/worker"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/auth"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/config"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/notify"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/vpn"
	"github.com/falahlaz/find-bugs-web/apps/server/web"
)

var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel()})))
	args := os.Args[1:]
	var err error
	switch {
	case len(args) == 0 || args[0] == "serve":
		err = serve()
	case args[0] == "user":
		err = userCmd(args[1:])
	case args[0] == "openapi":
		err = printOpenAPI()
	case args[0] == "version":
		fmt.Println(version)
	default:
		err = fmt.Errorf("unknown command %q (serve, user, openapi, version)", args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func logLevel() slog.Level {
	if strings.EqualFold(os.Getenv("LOG_LEVEL"), "debug") {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func serve() error {
	cfg, err := config.Load(true)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(dirOf(cfg.DBPath), 0o700); err != nil {
		return err
	}
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	// Stop background loops before closing the database.
	defer func() {
		stop()
		st.Close()
	}()

	notifier := notify.NewTelegram(cfg.TelegramToken, cfg.TelegramChatID)
	gp := vpn.NewManager(vpn.Config{Bin: cfg.GP.Bin, Portal: cfg.GP.Portal, Dir: cfg.GP.Dir, Browser: cfg.GP.Browser, ReachHosts: cfg.GP.ReachHosts, RestartDaemons: cfg.GP.RestartDaemons})
	sp := splunk.New(cfg.Splunk)
	if err := sp.Load(); err != nil {
		slog.Warn("splunk session not loaded; re-auth from the Splunk panel", "err", err)
	}
	mon := watchdog.New(gp, sp, notifier, cfg.PublicURL)

	claude := analyzer.ClaudeCode{Bin: cfg.Claude.Bin, Model: cfg.Claude.Model, Timeout: cfg.Claude.Timeout}
	var an analyzer.Analyzer = claude
	if cfg.LogAnalyzer == "antigravity" {
		agy := analyzer.Antigravity{Bin: cfg.Agy.Bin, Model: cfg.Agy.Model, Timeout: cfg.Agy.Timeout, StateDir: cfg.Agy.StateDir}
		an = agy
		if cfg.Agy.Fallback {
			an = analyzer.Fallback{Primary: agy, Secondary: claude, OnFallback: func(err error) {
				slog.Warn("antigravity log analysis failed; falling back to claude", "err", err)
			}}
		}
	}
	var tracer analyzer.Tracer = analyzer.ClaudeCode{Bin: cfg.Claude.Bin, Model: cfg.GitLab.Model, Timeout: cfg.GitLab.TraceTimeout}
	if cfg.Analyzer == "fake" {
		an, tracer = analyzer.Fake{}, analyzer.Fake{}
	}
	wk := worker.New(st, sp, an, mon, notifier, worker.Config{
		WaitingExpiry: cfg.WaitingExpiry, RecheckAfter: cfg.JobRecheckAfter, JobTimeout: cfg.JobTimeout, PublicURL: cfg.PublicURL,
		LogDir: logDir(cfg), CorrelationMaxIDs: cfg.Splunk.CorrelationMaxIDs,
		GitLabURL: cfg.GitLab.URL, TraceMaxRepos: cfg.GitLab.MaxRepos, EnvMap: cfg.GitLab.EnvMap,
		SessionDir: dirOf(cfg.DBPath) + "/trace-sessions",
	})
	var chat *tracechat.Service
	// One Manager for tracing and the Repo page, so work on a repo is
	// serialised across both.
	g := cfg.GitLab
	rm := repos.New(repos.Config{
		URL: g.URL, Username: g.Username, Token: g.Token, CAFile: g.CAFile, SkipTLSVerify: g.SkipTLSVerify,
		Dir: g.ReposDir, Group: g.Group, RepoMap: g.RepoMap, Ref: g.Ref, Timeout: g.Timeout, CloneTimeout: g.CloneTimeout,
	})
	var gl *gitlab.Client
	if g.CanClone() {
		if gl, err = gitlab.New(gitlab.Config{URL: g.URL, Token: g.Token, CAFile: g.CAFile, SkipTLSVerify: g.SkipTLSVerify}); err != nil {
			return err
		}
	}
	if g.Enabled() {
		wk.Repos = rm
		wk.Tracer = tracer
		wk.Deploys = gl
		chat = tracechat.New(st, tracer, rm, gl, tracechat.Config{
			GitLabURL: g.URL, Envs: config.Environments(g.EnvMap), Idle: g.SessionIdle, Retention: g.SessionRetention,
			WorktreeTTL: g.WorktreeTTL, Concurrency: g.ChatConcurrency, LogDir: logDir(cfg),
		}, ctx)
		if err := chat.Recover(ctx); err != nil {
			return err
		}
		if g.ConfigProject != "" {
			src := &configrepo.Source{
				Cfg:   configrepo.Config{Project: g.ConfigProject, Path: g.ConfigPath, Branches: g.ConfigBranches, JobPrefix: g.ConfigDeployJob},
				Repos: rm, Deploys: gl,
			}
			wk.Config, chat.ConfigRepo = src, src
		}
		slog.Info("code tracing enabled", "gitlab", g.URL, "dir", g.ReposDir, "ref", g.Ref, "model", g.Model, "env_map", g.EnvMap,
			"config_repo", g.ConfigProject, "config_branches", g.ConfigBranches)
	} else {
		slog.Info("code tracing disabled (set GITLAB_URL and GITLAB_TOKEN to enable)")
	}
	if err := wk.Recover(ctx); err != nil {
		return err
	}

	webFS, err := web.FS(envOr("WEB_DIR", "web/dist"))
	if err != nil {
		return err
	}
	if webFS == nil {
		slog.Warn("no frontend build found; only the API is served")
	}
	a := &api.API{
		Cfg: cfg, Store: st, Auth: auth.NewService(st, cfg.SessionTTL, cfg.CookieSecure),
		Jobs: &jobs.Service{
			Store: st, Environments: cfg.Splunk.Environments(), Header: cfg.TransactionIDHeader,
			Limits: store.Limits{Total: cfg.QueueMax, PerUser: cfg.QueuePerUser}, DedupWindow: cfg.DedupWindow, Wake: wk.Wake,
		},
		VPN: gp, Splunk: sp, Monitor: mon, Web: webFS, Version: version, BaseCtx: ctx, LogDir: logDir(cfg),
		Tools: tools.New(cfg.TselCiphersFile, cfg.TselPrivateKey, cfg.Location),
	}
	if chat != nil {
		a.Trace = chat
	}
	if nr, ok := an.(analyzer.NoteReviewer); ok {
		a.Notes = nr
	}
	if gl != nil {
		a.MRs = &mrtriage.Service{GL: gl, Author: cfg.MRTriageAuthor}
	}
	if cfg.Analyzer == "claude" {
		a.Usage = &usage.Prober{Bin: cfg.Claude.Bin, Timeout: time.Minute, MaxAge: 5 * time.Minute, MinGap: 30 * time.Second}
	}
	if fi, err := os.Stat(cfg.Agy.Bin); err == nil && !fi.IsDir() {
		a.AgyUsage = &usage.AgyProber{Bin: cfg.Agy.Bin, StateDir: cfg.Agy.StateDir, Timeout: time.Minute, MaxAge: 5 * time.Minute, MinGap: 30 * time.Second}
	}
	if g.ReposDir != "" {
		a.Repos = rm
		a.RC = rcsession.New(rcsession.Config{
			Script: cfg.RCSession.Script, Launcher: cfg.RCSession.Launcher, ClaudeBin: cfg.Claude.Bin, AgyBin: cfg.Agy.Bin, Root: g.ReposDir,
		})
		if !a.RC.Enabled() {
			slog.Info("rc sessions disabled (no rc-session script)", "script", cfg.RCSession.Script)
		}
	}

	go mon.Run(ctx, cfg.WatchInterval)
	go wk.Run(ctx)
	go maintenance(ctx, st, cfg, chat)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.ListenAddr, "version", version)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// maintenance purges expired sessions and log files hourly and applies the
// other retention daily.
func maintenance(ctx context.Context, st *store.Store, cfg config.Config, chat *tracechat.Service) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	var lastRetention time.Time
	for {
		if err := st.PurgeExpiredSessions(ctx); err != nil && ctx.Err() == nil {
			slog.Error("purge sessions", "err", err)
		}
		if chat != nil {
			if n, w, err := chat.Cleanup(ctx); err != nil && ctx.Err() == nil {
				slog.Error("trace session cleanup", "err", err)
			} else if n > 0 || w > 0 {
				slog.Info("trace cleanup", "sessions_purged", n, "worktrees_removed", w)
			}
		}
		// Log files are purged hourly so none outlives RetainLogFiles by more
		// than an hour.
		if n, err := worker.PurgeLogFiles(logDir(cfg), time.Now().Add(-cfg.RetainLogFiles)); err != nil && ctx.Err() == nil {
			slog.Error("retention log files", "err", err)
		} else if n > 0 {
			slog.Info("retention: log files purged", "count", n)
		}
		if time.Since(lastRetention) >= 24*time.Hour {
			now := time.Now()
			if n, err := st.PurgeRawLogs(ctx, now.Add(-cfg.RetainRawLogs)); err != nil {
				slog.Error("retention raw logs", "err", err)
			} else if n > 0 {
				slog.Info("retention: raw logs purged", "count", n)
			}
			if n, err := st.PurgeDiagnoses(ctx, now.Add(-cfg.RetainDiagnosis)); err != nil {
				slog.Error("retention diagnoses", "err", err)
			} else if n > 0 {
				slog.Info("retention: diagnoses purged", "count", n)
			}
			lastRetention = now
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// logDir holds the per-job Splunk result files, next to the database.
func logDir(cfg config.Config) string { return dirOf(cfg.DBPath) + "/logs" }

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return "."
}

func openStore(ctx context.Context) (*store.Store, error) {
	cfg, err := config.Load(false)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dirOf(cfg.DBPath), 0o700); err != nil {
		return nil, err
	}
	return store.Open(ctx, cfg.DBPath)
}

func userCmd(args []string) error {
	ctx := context.Background()
	usage := errors.New("usage: server user create <username> <qa|engineer> | server user passwd <username>")
	if len(args) < 2 {
		return usage
	}
	st, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	switch args[0] {
	case "create":
		if len(args) != 3 || !store.ValidRole(args[2]) {
			return usage
		}
		pw, err := readPassword()
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return err
		}
		u, err := st.CreateUser(ctx, args[1], hash, args[2])
		if err != nil {
			return err
		}
		_ = st.AddAudit(ctx, 0, "user.create", "ok", u.Username+" ("+u.Role+") via CLI")
		fmt.Printf("created user %s (%s)\n", u.Username, u.Role)
	case "passwd":
		if len(args) != 2 {
			return usage
		}
		u, err := st.GetUserByUsername(ctx, args[1])
		if err != nil {
			return fmt.Errorf("user %s: %w", args[1], err)
		}
		pw, err := readPassword()
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return err
		}
		if _, err := st.UpdateUser(ctx, u.ID, store.UserUpdate{PasswordHash: &hash}); err != nil {
			return err
		}
		_ = st.DeleteUserSessions(ctx, u.ID)
		_ = st.AddAudit(ctx, 0, "user.passwd", "ok", u.Username+" via CLI")
		fmt.Printf("password updated for %s\n", u.Username)
	default:
		return usage
	}
	return nil
}

// readPassword prompts twice on a terminal, or reads one line from stdin.
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "Password: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Ulangi password: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("password tidak sama")
	}
	return string(a), nil
}

func printOpenAPI() error {
	cfg, _ := config.Load(false)
	a := &api.API{Cfg: cfg, Version: version}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(a.OpenAPI())
}
