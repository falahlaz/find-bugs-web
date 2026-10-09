package api

import (
	"net/http"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/usage"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
)

func (a *API) registerUsageRoutes() {
	a.add(route{method: "GET", path: "/api/usage", summary: "Claude subscription usage: 5-hour and weekly windows (refresh=1 re-probes)", tag: "system", opID: "claudeUsage",
		roles: []string{engineer}, query: []string{"refresh"},
		resps: map[int]any{200: usage.Snapshot{}}, h: a.claudeUsage})
	a.add(route{method: "GET", path: "/api/usage/agy", summary: "Antigravity (agy) quota per model pool (refresh=1 re-probes)", tag: "system", opID: "agyUsage",
		roles: []string{engineer}, query: []string{"refresh"},
		resps: map[int]any{200: usage.AgySnapshot{}}, h: a.agyUsage})
}

func (a *API) claudeUsage(w http.ResponseWriter, r *http.Request) {
	if a.Usage == nil {
		httpx.Error(w, http.StatusNotFound, "usage_disabled", "Pemakaian Claude tidak tersedia (analyzer bukan Claude).")
		return
	}
	s, err := a.Usage.Get(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "usage_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}

func (a *API) agyUsage(w http.ResponseWriter, r *http.Request) {
	if a.AgyUsage == nil {
		httpx.Error(w, http.StatusNotFound, "usage_disabled", "Pemakaian agy tidak tersedia (agy tidak terpasang).")
		return
	}
	s, err := a.AgyUsage.Get(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "usage_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}
