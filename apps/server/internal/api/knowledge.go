package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/knowledge"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

// reportCSP sandboxes report.html into an opaque origin: its inline script
// runs but can't read the session cookie or call the API.
const reportCSP = "sandbox allow-scripts allow-popups allow-modals; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'self'; base-uri 'none'; form-action 'none'"

func (a *API) registerReportRoutes() {
	a.add(route{method: "GET", path: "/api/reports", summary: "Trace reports in KNOWLEDGE_DIR", tag: "reports", opID: "listReports",
		resps: map[int]any{200: ReportsResponse{}}, h: a.listReports})
	a.add(route{method: "GET", path: "/api/reports/archive.zip", summary: "Knowledge base as a zip (an Obsidian vault); ?repo= limits it to one repo", tag: "reports", roles: []string{engineer},
		raw: true, h: a.downloadReportsZip})
	a.add(route{method: "GET", path: "/api/reports/{repo}/{slug}/{file}", summary: "report.html (all) or report.md (engineer); ?download=1 for an attachment", tag: "reports",
		raw: true, h: a.reportFile})
}

func (a *API) knowledgeEnabled(w http.ResponseWriter) bool {
	if a.Cfg.KnowledgeDir == "" {
		httpx.Error(w, http.StatusNotFound, "knowledge_disabled", "KNOWLEDGE_DIR tidak dikonfigurasi di server ini.")
		return false
	}
	return true
}

func isEngineer(r *http.Request) bool { return identity(r).User.Role == store.RoleEngineer }

func (a *API) listReports(w http.ResponseWriter, r *http.Request) {
	if !a.knowledgeEnabled(w) {
		return
	}
	list, err := knowledge.List(a.Cfg.KnowledgeDir)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	eng := isEngineer(r)
	out := ReportsResponse{Reports: []ReportView{}, CanReadMD: eng}
	for _, rp := range list {
		if !eng && !rp.HasHTML {
			continue
		}
		out.Reports = append(out.Reports, ReportView{
			Repo: rp.Repo, Slug: rp.Slug, Title: rp.Title, Date: rp.Date, Status: rp.Status, Severity: rp.Severity,
			Summary: rp.Summary, Tags: nonNil(rp.Tags), Endpoints: nonNil(rp.Endpoints),
			HasMD: eng && rp.HasMD, HasHTML: rp.HasHTML, UpdatedAt: rp.Updated,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (a *API) reportFile(w http.ResponseWriter, r *http.Request) {
	if !a.knowledgeEnabled(w) {
		return
	}
	repo, slug, file := r.PathValue("repo"), r.PathValue("slug"), r.PathValue("file")
	if file == knowledge.MD && !isEngineer(r) {
		httpx.Error(w, http.StatusForbidden, "forbidden", "Laporan teknis hanya untuk engineer.")
		return
	}
	f, err := knowledge.Open(a.Cfg.KnowledgeDir, repo, slug, file)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "Laporan tidak ditemukan.")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	if file == knowledge.HTML {
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Security-Policy", reportCSP)
		h.Set("X-Frame-Options", "SAMEORIGIN")
	} else {
		h.Set("Content-Type", "text/markdown; charset=utf-8")
	}
	if r.URL.Query().Get("download") == "1" {
		a.audit(r, "report.download", "ok", repo+"/"+slug+"/"+file)
		h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s%s"`, slug, fileExt(file)))
	}
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func fileExt(file string) string {
	if file == knowledge.MD {
		return ".md"
	}
	return ".html"
}

func (a *API) downloadReportsZip(w http.ResponseWriter, r *http.Request) {
	if !a.knowledgeEnabled(w) {
		return
	}
	repo := r.URL.Query().Get("repo")
	if repo != "" && !knowledge.ValidName(repo) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Repo tidak ditemukan.")
		return
	}
	if _, err := os.Stat(a.Cfg.KnowledgeDir); err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "Knowledge base masih kosong.")
		return
	}
	name := "knowledge"
	if repo != "" {
		name += "-" + repo
		if fi, err := os.Stat(filepath.Join(a.Cfg.KnowledgeDir, repo)); err != nil || !fi.IsDir() {
			httpx.Error(w, http.StatusNotFound, "not_found", "Repo tidak ditemukan.")
			return
		}
	}
	a.audit(r, "report.archive", "ok", name)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
	w.Header().Set("Cache-Control", "no-store")
	// Headers are sent by now; a truncated zip is all the client sees.
	if err := knowledge.WriteZip(w, a.Cfg.KnowledgeDir, repo); err != nil {
		slog.Warn("knowledge zip", "repo", repo, "err", err)
	}
}
