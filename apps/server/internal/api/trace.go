package api

import (
	"errors"
	"net/http"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/tracechat"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

func (a *API) registerTraceRoutes() {
	eng := []string{engineer}
	view := map[int]any{200: TraceSessionView{}}
	a.add(route{method: "GET", path: "/api/jobs/{id}/trace", summary: "Code trace chat", tag: "trace", opID: "getTrace", roles: eng,
		resps: view, h: a.getTrace})
	a.add(route{method: "POST", path: "/api/jobs/{id}/trace/messages", summary: "Ask about the code trace", tag: "trace", opID: "askTrace", roles: eng,
		req: TraceAskRequest{}, resps: map[int]any{202: TraceSessionView{}}, h: a.askTrace})
	a.add(route{method: "POST", path: "/api/jobs/{id}/trace/retrace", summary: "Trace the error in another version", tag: "trace", opID: "retrace", roles: eng,
		req: TraceRetraceRequest{}, resps: map[int]any{202: TraceSessionView{}}, h: a.retrace})
	a.add(route{method: "POST", path: "/api/jobs/{id}/trace/close", summary: "Close the code trace chat", tag: "trace", opID: "closeTrace", roles: eng,
		resps: view, h: a.closeTrace})
	a.add(route{method: "POST", path: "/api/jobs/{id}/trace/reopen", summary: "Reopen the code trace chat", tag: "trace", opID: "reopenTrace", roles: eng,
		resps: view, h: a.reopenTrace})
	a.add(route{method: "GET", path: "/api/jobs/{id}/trace/refs", summary: "Deployed commits and branches to re-trace into", tag: "trace", opID: "traceRefs", roles: eng,
		query: []string{"project", "q"}, resps: map[int]any{200: TraceRefsResponse{}}, h: a.traceRefs})
}

// traceJob returns the job ID, or writes an error if tracing is off.
func (a *API) traceJob(w http.ResponseWriter, r *http.Request) (int64, bool) {
	if a.Trace == nil {
		httpx.Error(w, http.StatusNotFound, "trace_disabled", "Code trace tidak aktif di server ini.")
		return 0, false
	}
	return pathID(w, r)
}

// traceError maps service errors to responses; it reports whether err was nil.
func traceError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not_found", "Job ini tidak punya sesi trace.")
	case errors.Is(err, tracechat.ErrBusy):
		httpx.Error(w, http.StatusConflict, "busy", err.Error())
	case errors.Is(err, tracechat.ErrClosed):
		httpx.Error(w, http.StatusConflict, "closed", err.Error())
	case errors.Is(err, tracechat.ErrBadRequest):
		httpx.Error(w, http.StatusBadRequest, "bad_request", err.Error())
	default:
		httpx.Internal(w, r, err)
	}
	return false
}

func (a *API) writeTrace(w http.ResponseWriter, r *http.Request, id int64, code int) {
	v, err := a.Trace.Get(r.Context(), id)
	if !traceError(w, r, err) {
		return
	}
	out := TraceSessionView{
		State: v.State, Busy: v.Busy, IdleClosed: v.IdleClosed, IdleMinutes: int(a.Trace.Cfg.Idle.Minutes()),
		LastActivityAt: v.LastActivityAt, Envs: v.Envs, Messages: v.Messages, Repos: []TraceRepoView{},
	}
	if out.Envs == nil {
		out.Envs = []string{}
	}
	for _, rp := range v.Repos {
		out.Repos = append(out.Repos, TraceRepoView{Project: rp.Project, Commit: rp.Commit, Ref: rp.Ref, RefSource: rp.RefSource, Env: rp.Env})
	}
	httpx.JSON(w, code, out)
}

func (a *API) getTrace(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.traceJob(w, r); ok {
		a.writeTrace(w, r, id, http.StatusOK)
	}
}

func (a *API) askTrace(w http.ResponseWriter, r *http.Request) {
	id, ok := a.traceJob(w, r)
	if !ok {
		return
	}
	var req TraceAskRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if traceError(w, r, a.Trace.Ask(r.Context(), id, identity(r).User.ID, req.Text)) {
		a.writeTrace(w, r, id, http.StatusAccepted)
	}
}

func (a *API) retrace(w http.ResponseWriter, r *http.Request) {
	id, ok := a.traceJob(w, r)
	if !ok {
		return
	}
	var req TraceRetraceRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	err := a.Trace.Retrace(r.Context(), id, identity(r).User.ID, tracechat.RetraceRequest{Project: req.Project, Env: req.Env, Ref: req.Ref})
	if traceError(w, r, err) {
		a.writeTrace(w, r, id, http.StatusAccepted)
	}
}

func (a *API) closeTrace(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.traceJob(w, r); ok && traceError(w, r, a.Trace.Close(r.Context(), id)) {
		a.writeTrace(w, r, id, http.StatusOK)
	}
}

func (a *API) reopenTrace(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.traceJob(w, r); ok && traceError(w, r, a.Trace.Reopen(r.Context(), id)) {
		a.writeTrace(w, r, id, http.StatusOK)
	}
}

func (a *API) traceRefs(w http.ResponseWriter, r *http.Request) {
	id, ok := a.traceJob(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	refs, err := a.Trace.Refs(r.Context(), id, q.Get("project"), q.Get("q"))
	if !traceError(w, r, err) {
		return
	}
	out := TraceRefsResponse{Deployments: []TraceEnvDeployment{}, Branches: []TraceBranch{}}
	for _, d := range refs.Deployments {
		v := TraceEnvDeployment{Env: d.Env, Ref: d.Ref, Commit: d.Commit, Error: d.Err}
		if !d.FinishedAt.IsZero() {
			t := d.FinishedAt
			v.FinishedAt = &t
		}
		out.Deployments = append(out.Deployments, v)
	}
	for _, b := range refs.Branches {
		out.Branches = append(out.Branches, TraceBranch{Name: b.Name, Commit: b.Commit})
	}
	httpx.JSON(w, http.StatusOK, out)
}
