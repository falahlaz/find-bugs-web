package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/mrtriage"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/repos"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
)

func (a *API) registerMRRoutes() {
	eng := []string{engineer}
	a.add(route{method: "GET", path: "/api/mrs", summary: "The GitLab token owner's MRs: Ready / Conflicts / Comments / Drafts (state=all adds merged and closed)", tag: "mrs", opID: "listMRs",
		roles: eng, query: []string{"state"}, resps: map[int]any{200: MRTriageResponse{}}, h: a.listMRs})
	a.add(route{method: "GET", path: "/api/mrs/{project}/{iid}/comments", summary: "Unresolved comment threads of an MR", tag: "mrs", opID: "mrComments",
		roles: eng, resps: map[int]any{200: MRCommentsResponse{}}, h: a.mrComments})
	a.add(route{method: "GET", path: "/api/mrs/{project}/{iid}/conflicts", summary: "Files that conflict, from a merge in the repo under GITLAB_REPOS_DIR", tag: "mrs", opID: "mrConflicts",
		roles: eng, resps: map[int]any{200: MRConflictsResponse{}}, h: a.mrConflicts})
	a.add(route{method: "POST", path: "/api/mrs/{project}/{iid}/merge", summary: "Merge an MR that is still ready and remove its source branch", tag: "mrs", opID: "mergeMR",
		roles: eng, resps: map[int]any{200: MRMergeResponse{}}, h: a.mergeMR})
}

func (a *API) mrsEnabled(w http.ResponseWriter) bool {
	if a.MRs == nil {
		httpx.Error(w, http.StatusNotFound, "mrs_disabled", "MR Triage butuh GITLAB_URL dan GITLAB_TOKEN.")
		return false
	}
	return true
}

// mrPathIDs reads the {project} and {iid} path values.
func mrPathIDs(w http.ResponseWriter, r *http.Request) (project, iid int64, ok bool) {
	project, err1 := strconv.ParseInt(r.PathValue("project"), 10, 64)
	iid, err2 := strconv.ParseInt(r.PathValue("iid"), 10, 64)
	if err1 != nil || err2 != nil || project <= 0 || iid <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Project ID dan IID harus angka.")
		return 0, 0, false
	}
	return project, iid, true
}

// gitlabError reports a failed GitLab call.
func gitlabError(w http.ResponseWriter, err error) {
	var se *gitlab.StatusError
	switch {
	case errors.As(err, &se) && (se.Code == 405 || se.Code == 406 || se.Code == 409 || se.Code == 422):
		// GitLab refused the merge (not mergeable, pipeline, branch moved).
		httpx.Error(w, http.StatusConflict, "gitlab_refused", "GitLab menolak: "+se.Msg)
	case se != nil:
		httpx.Error(w, http.StatusBadGateway, "gitlab_failed", err.Error())
	case errors.Is(err, gitlab.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not_found", "MR tidak ditemukan di GitLab.")
	case errors.Is(err, gitlab.ErrForbidden):
		httpx.Error(w, http.StatusForbidden, "gitlab_forbidden", err.Error())
	default:
		httpx.Error(w, http.StatusBadGateway, "gitlab_failed", "GitLab tidak bisa dihubungi (VPN?): "+err.Error())
	}
}

func mrView(m mrtriage.MR, local map[string]*RepoView) MRView {
	v := MRView{
		ProjectID: m.ProjectID, IID: m.IID, Project: m.ProjectPath(), Title: m.Title, State: m.State, Draft: m.Draft,
		HasConflicts: m.HasConflicts, MergeStatus: m.DetailedMergeStatus, Notes: m.UserNotesCount, Source: m.SourceBranch, Target: m.TargetBranch,
		WebURL: m.WebURL, UpdatedAt: m.UpdatedAt, Repo: local[m.ProjectPath()],
	}
	if ap := m.Approvals; ap != nil {
		v.Approvals = &MRApprovalView{Approved: ap.Approved, Required: ap.Required, Left: ap.Left, ApprovedBy: ap.ApprovedBy}
	}
	return v
}

// localRepos maps a project to its repo under GITLAB_REPOS_DIR and its
// Remote Control session.
func (a *API) localRepos(r *http.Request) map[string]*RepoView {
	out := map[string]*RepoView{}
	if a.Repos == nil {
		return out
	}
	list, err := a.Repos.List(r.Context())
	if err != nil {
		return out
	}
	sessions := a.sessions(r.Context())
	for _, rp := range list {
		v := repoView(rp, sessions)
		out[rp.Project] = &v
	}
	return out
}

func (a *API) listMRs(w http.ResponseWriter, r *http.Request) {
	if !a.mrsEnabled(w) {
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "opened"
	}
	if state != "opened" && state != "all" {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "state harus opened atau all.")
		return
	}
	t, err := a.MRs.Triage(r.Context(), state)
	if err != nil {
		gitlabError(w, err)
		return
	}
	local := a.localRepos(r)
	views := func(ms []mrtriage.MR) []MRView {
		out := make([]MRView, len(ms))
		for i, m := range ms {
			out[i] = mrView(m, local)
		}
		return out
	}
	httpx.JSON(w, http.StatusOK, MRTriageResponse{
		Author: a.MRs.Author, Ready: views(t.Ready), Waiting: views(t.Waiting), Conflict: views(t.Conflict),
		Comments: views(t.Comments), Drafts: views(t.Drafts), Others: views(t.Others),
	})
}

func (a *API) mrComments(w http.ResponseWriter, r *http.Request) {
	if !a.mrsEnabled(w) {
		return
	}
	project, iid, ok := mrPathIDs(w, r)
	if !ok {
		return
	}
	cs, err := a.MRs.UnresolvedComments(r.Context(), project, iid)
	if err != nil {
		gitlabError(w, err)
		return
	}
	out := MRCommentsResponse{Comments: make([]MRCommentView, len(cs))}
	for i, c := range cs {
		out.Comments[i] = MRCommentView{Path: c.Path, Line: c.Line, Author: c.Author, Body: c.Body, Replies: c.Replies}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) mrConflicts(w http.ResponseWriter, r *http.Request) {
	if !a.mrsEnabled(w) || !a.reposEnabled(w) {
		return
	}
	project, iid, ok := mrPathIDs(w, r)
	if !ok {
		return
	}
	mr, err := a.MRs.GL.MergeRequest(r.Context(), project, iid)
	if err != nil {
		gitlabError(w, err)
		return
	}
	files, err := a.Repos.MergeConflicts(r.Context(), mr.ProjectPath(), mr.SourceBranch, mr.TargetBranch)
	switch {
	case errors.Is(err, repos.ErrNotCloned):
		httpx.Error(w, http.StatusNotFound, "repo_not_cloned", "Repo "+mr.ProjectPath()+" belum di-clone. Clone dulu dari halaman Repo.")
		return
	case err != nil:
		httpx.Error(w, http.StatusBadGateway, "conflicts_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, MRConflictsResponse{Files: files})
}

func (a *API) mergeMR(w http.ResponseWriter, r *http.Request) {
	if !a.mrsEnabled(w) {
		return
	}
	project, iid, ok := mrPathIDs(w, r)
	if !ok {
		return
	}
	mr, err := a.MRs.Merge(r.Context(), project, iid)
	ref := mr.References.Full
	if ref == "" {
		ref = strconv.FormatInt(project, 10) + "!" + strconv.FormatInt(iid, 10)
	}
	if err != nil {
		a.audit(r, "mr.merge", "error", ref+": "+err.Error())
		if errors.Is(err, mrtriage.ErrNotReady) {
			httpx.Error(w, http.StatusConflict, "not_ready", err.Error())
			return
		}
		gitlabError(w, err)
		return
	}
	a.audit(r, "mr.merge", "ok", ref)
	httpx.JSON(w, http.StatusOK, MRMergeResponse{MR: mrView(mrtriage.MR{MergeRequest: mr}, nil)})
}
