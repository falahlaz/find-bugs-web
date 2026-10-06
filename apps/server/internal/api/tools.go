package api

import (
	"errors"
	"net/http"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools"
)

func (a *API) registerToolRoutes() {
	a.add(route{method: "GET", path: "/api/tools", menu: store.MenuTools, summary: "MyTelkomsel support tools and their form fields", tag: "tools", opID: "listTools",
		resps: map[int]any{200: ToolsResponse{}}, h: a.listTools})
	a.add(route{method: "POST", path: "/api/tools/{tool}/run", menu: store.MenuTools, summary: "Run a tool with the form values (400 bad input, 422 the tool refused)", tag: "tools", opID: "runTool",
		req: ToolInput{}, resps: map[int]any{200: tools.ToolResult{}}, h: a.runTool})
}

func (a *API) toolsEnabled(w http.ResponseWriter) bool {
	if a.Tools == nil {
		httpx.Error(w, http.StatusNotFound, "tools_disabled", "Tools tidak aktif di server ini.")
		return false
	}
	return true
}

func (a *API) listTools(w http.ResponseWriter, _ *http.Request) {
	if !a.toolsEnabled(w) {
		return
	}
	httpx.JSON(w, http.StatusOK, ToolsResponse{Tools: a.Tools.List()})
}

func (a *API) runTool(w http.ResponseWriter, r *http.Request) {
	if !a.toolsEnabled(w) {
		return
	}
	in := ToolInput{}
	if !httpx.Decode(w, r, &in) {
		return
	}
	id := r.PathValue("tool")
	res, err := a.Tools.Run(id, in)
	// Inputs and outputs carry production secrets, so only the tool is audited.
	var inputErr *tools.InputError
	var userErr *tools.UserError
	switch {
	case err == nil:
		a.audit(r, "tool.run", "ok", id)
		httpx.JSON(w, http.StatusOK, res)
	case errors.Is(err, tools.ErrUnknownTool):
		httpx.Error(w, http.StatusNotFound, "not_found", "Tool tidak ditemukan.")
	case errors.As(err, &inputErr):
		httpx.Error(w, http.StatusBadRequest, "bad_input", err.Error())
	case errors.As(err, &userErr):
		a.audit(r, "tool.run", "error", id)
		httpx.Error(w, http.StatusUnprocessableEntity, "tool_error", err.Error())
	default:
		a.audit(r, "tool.run", "error", id)
		httpx.Internal(w, r, err)
	}
}
