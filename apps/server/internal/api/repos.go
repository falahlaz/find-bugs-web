package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/rcsession"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
)

// cloneState tracks the clones started from the Repo page. Finished clones
// are dropped (the repo shows in the list); failed ones stay until retried.
type cloneState struct {
	mu     sync.Mutex
	clones map[string]*RepoCloneView
}

func (a *API) registerRepoRoutes() {
	eng := []string{engineer}
	a.add(route{method: "GET", path: "/api/repos", summary: "Repos under GITLAB_REPOS_DIR and their rc sessions", tag: "repos", opID: "listRepos", roles: eng,
		resps: map[int]any{200: ReposResponse{}}, h: a.listRepos})
	a.add(route{method: "POST", path: "/api/repos", summary: "Clone a GitLab project (in the background)", tag: "repos", opID: "cloneRepo", roles: eng,
		req: RepoRequest{}, resps: map[int]any{202: RepoCloneView{}}, h: a.cloneRepo})
	a.add(route{method: "POST", path: "/api/repos/session/start", summary: "Start a Claude Remote Control session in a repo", tag: "repos", opID: "startRepoSession", roles: eng,
		req: RepoRequest{}, resps: map[int]any{200: RepoSessionResponse{}}, h: a.startRepoSession})
	a.add(route{method: "POST", path: "/api/repos/session/stop", summary: "Stop a repo's Remote Control session", tag: "repos", opID: "stopRepoSession", roles: eng,
		req: RepoRequest{}, resps: map[int]any{200: RepoSessionResponse{}}, h: a.stopRepoSession})
}

func (a *API) reposEnabled(w http.ResponseWriter) bool {
	if a.Repos == nil {
		httpx.Error(w, http.StatusNotFound, "repos_disabled", "Direktori repo tidak dikonfigurasi di server ini.")
		return false
	}
	return true
}

func (a *API) sessionsEnabled(w http.ResponseWriter) bool {
	if !a.reposEnabled(w) {
		return false
	}
	if !a.RC.Enabled() {
		httpx.Error(w, http.StatusNotFound, "rc_disabled", "Script rc-session tidak ada di server ini.")
		return false
	}
	return true
}

func (a *API) sessions(ctx context.Context) map[string]rcsession.Session {
	if !a.RC.Enabled() {
		return nil
	}
	s, err := a.RC.Sessions(ctx)
	if err != nil {
		slog.Warn("list rc sessions", "err", err)
	}
	return s
}

func repoView(r repos.Repo, sessions map[string]rcsession.Session) RepoView {
	v := RepoView{Project: r.Project, Branch: r.Branch, Commit: r.Commit, Subject: r.Subject, Shallow: r.Shallow}
	if !r.CommittedAt.IsZero() {
		t := r.CommittedAt
		v.CommittedAt = &t
	}
	if s, ok := sessions[r.Dir]; ok {
		v.Session = &RepoSessionView{Name: s.Name, URL: s.URL, Since: s.Since}
	}
	return v
}

func (a *API) listRepos(w http.ResponseWriter, r *http.Request) {
	if !a.reposEnabled(w) {
		return
	}
	list, err := a.Repos.List(r.Context())
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	sessions := a.sessions(r.Context())
	out := ReposResponse{
		Repos: []RepoView{}, Clones: []RepoCloneView{}, DefaultGroup: a.Cfg.GitLab.Group,
		CanClone: a.Cfg.GitLab.CanClone(), CanSession: a.RC.Enabled(),
	}
	for _, rp := range list {
		out.Repos = append(out.Repos, repoView(rp, sessions))
	}
	a.cloning.mu.Lock()
	for _, c := range a.cloning.clones {
		out.Clones = append(out.Clones, *c)
	}
	a.cloning.mu.Unlock()
	sort.Slice(out.Clones, func(i, j int) bool { return out.Clones[i].StartedAt.Before(out.Clones[j].StartedAt) })
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) cloneRepo(w http.ResponseWriter, r *http.Request) {
	if !a.reposEnabled(w) {
		return
	}
	if !a.Cfg.GitLab.CanClone() {
		httpx.Error(w, http.StatusNotFound, "clone_disabled", "GITLAB_URL dan GITLAB_TOKEN belum diset.")
		return
	}
	var req RepoRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	project, ok := a.Repos.ProjectPath(req.Project)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Nama project tidak valid. Pakai group/project atau nama service.")
		return
	}
	list, err := a.Repos.List(r.Context())
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	for _, rp := range list {
		if rp.Project == project {
			httpx.Error(w, http.StatusConflict, "exists", "Repo "+project+" sudah ada.")
			return
		}
	}
	u := identity(r).User
	a.cloning.mu.Lock()
	if a.cloning.clones == nil {
		a.cloning.clones = map[string]*RepoCloneView{}
	}
	if c := a.cloning.clones[project]; c != nil && c.State == "cloning" {
		a.cloning.mu.Unlock()
		httpx.Error(w, http.StatusConflict, "cloning", "Repo "+project+" sedang di-clone.")
		return
	}
	c := &RepoCloneView{Project: project, State: "cloning", By: u.Username, StartedAt: time.Now()}
	a.cloning.clones[project] = c
	view := *c
	a.cloning.mu.Unlock()
	a.audit(r, "repo.clone", "started", project)

	// A full clone can outlast the request (and the tunnel's timeout).
	go func() {
		_, err := a.Repos.Clone(a.BaseCtx, project)
		result, detail := "ok", project
		a.cloning.mu.Lock()
		if err == nil || errors.Is(err, repos.ErrExists) {
			delete(a.cloning.clones, project)
		} else {
			c.State, c.Error = "failed", err.Error()
			result, detail = "failed", project+": "+err.Error()
		}
		a.cloning.mu.Unlock()
		if err != nil {
			slog.Warn("clone repo", "project", project, "err", err)
		}
		if err := a.Store.AddAudit(a.BaseCtx, u.ID, "repo.clone", result, detail); err != nil {
			slog.Error("audit", "err", err)
		}
	}()
	httpx.JSON(w, http.StatusAccepted, view)
}

// sessionRepo finds the repo a session request names.
func (a *API) sessionRepo(w http.ResponseWriter, r *http.Request) (repos.Repo, bool) {
	var req RepoRequest
	if !httpx.Decode(w, r, &req) {
		return repos.Repo{}, false
	}
	list, err := a.Repos.List(r.Context())
	if err != nil {
		httpx.Internal(w, r, err)
		return repos.Repo{}, false
	}
	for _, rp := range list {
		if rp.Project == req.Project {
			return rp, true
		}
	}
	httpx.Error(w, http.StatusNotFound, "not_found", "Repo tidak ditemukan.")
	return repos.Repo{}, false
}

func (a *API) startRepoSession(w http.ResponseWriter, r *http.Request) {
	a.repoSession(w, r, "repo.session_start", a.RC.Start)
}

func (a *API) stopRepoSession(w http.ResponseWriter, r *http.Request) {
	a.repoSession(w, r, "repo.session_stop", a.RC.Stop)
}

func (a *API) repoSession(w http.ResponseWriter, r *http.Request, action string, run func(context.Context, string) (string, error)) {
	if !a.sessionsEnabled(w) {
		return
	}
	rp, ok := a.sessionRepo(w, r)
	if !ok {
		return
	}
	// Starting waits for the session to connect, longer than the server's
	// write timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
	out, err := run(r.Context(), rp.Dir)
	if err != nil {
		a.audit(r, action, "failed", rp.Project+": "+err.Error())
		httpx.Error(w, http.StatusBadGateway, "rc_failed", err.Error())
		return
	}
	a.audit(r, action, "ok", rp.Project)
	httpx.JSON(w, http.StatusOK, RepoSessionResponse{Repo: repoView(rp, a.sessions(r.Context())), Output: out})
}
