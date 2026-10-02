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
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/jobs"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/watchdog"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/worker"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/auth"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/config"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/notify"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
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
	gp := vpn.NewManager(vpn.Config{Bin: cfg.GP.Bin, Portal: cfg.GP.Portal, Dir: cfg.GP.Dir, Browser: cfg.GP.Browser, ReachHosts: cfg.GP.ReachHosts})
	sp := splunk.New(cfg.Splunk)
	if err := sp.Load(); err != nil {
		slog.Warn("splunk session not loaded; re-auth from the Splunk panel", "err", err)
	}
	mon := watchdog.New(gp, sp, notifier, cfg.PublicURL)

	cc := analyzer.ClaudeCode{Bin: cfg.Claude.Bin, Model: cfg.Claude.Model, Timeout: cfg.Claude.Timeout}
	var an analyzer.Analyzer = cc
	var tracer analyzer.Tracer = cc
	if cfg.Analyzer == "fake" {
		an, tracer = analyzer.Fake{}, analyzer.Fake{}
	}
	wk := worker.New(st, sp, an, mon, notifier, worker.Config{
		WaitingExpiry: cfg.WaitingExpiry, RecheckAfter: cfg.JobRecheckAfter, JobTimeout: cfg.JobTimeout, PublicURL: cfg.PublicURL,
		LogDir: logDir(cfg), CorrelationMaxIDs: cfg.Splunk.CorrelationMaxIDs,
		GitLabURL: cfg.GitLab.URL, TraceMaxRepos: cfg.GitLab.MaxRepos,
	})
	if g := cfg.GitLab; g.Enabled() {
		wk.Repos = repos.New(repos.Config{
			URL: g.URL, Username: g.Username, Token: g.Token, CAFile: g.CAFile, SkipTLSVerify: g.SkipTLSVerify,
			Dir: g.ReposDir, Group: g.Group, RepoMap: g.RepoMap, Ref: g.Ref, Timeout: g.Timeout,
		})
		wk.Tracer = tracer
		slog.Info("code tracing enabled", "gitlab", g.URL, "dir", g.ReposDir, "ref", g.Ref)
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
		VPN: gp, Splunk: sp, Monitor: mon, Web: webFS, Version: version, BaseCtx: ctx,
	}

	go mon.Run(ctx, cfg.WatchInterval)
	go wk.Run(ctx)
	go maintenance(ctx, st, cfg)

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

// maintenance purges expired sessions hourly and applies retention daily.
func maintenance(ctx context.Context, st *store.Store, cfg config.Config) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	var lastRetention time.Time
	for {
		if err := st.PurgeExpiredSessions(ctx); err != nil && ctx.Err() == nil {
			slog.Error("purge sessions", "err", err)
		}
		if time.Since(lastRetention) >= 24*time.Hour {
			now := time.Now()
			if n, err := st.PurgeRawLogs(ctx, now.Add(-cfg.RetainRawLogs)); err != nil {
				slog.Error("retention raw logs", "err", err)
			} else if n > 0 {
				slog.Info("retention: raw logs purged", "count", n)
			}
			if n, err := worker.PurgeLogFiles(logDir(cfg), now.Add(-cfg.RetainRawLogs)); err != nil {
				slog.Error("retention log files", "err", err)
			} else if n > 0 {
				slog.Info("retention: log files purged", "count", n)
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
