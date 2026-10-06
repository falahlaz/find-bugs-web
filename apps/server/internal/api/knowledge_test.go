package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReports(t *testing.T) {
	h := newHarness(t)
	qa, eng := h.login("qa1"), h.login("eng")
	dir := t.TempDir()
	h.api.Cfg.KnowledgeDir = dir
	rd := filepath.Join(dir, "svc", "2026-10-06-cobe")
	os.MkdirAll(rd, 0o755)
	os.WriteFile(filepath.Join(rd, "report.md"), []byte("---\ntitle: COBE\ndate: 2026-10-06\ntags: a, b\n---\n# COBE"), 0o644)
	os.WriteFile(filepath.Join(rd, "report.html"), []byte("<html><script>1</script></html>"), 0o644)
	md := filepath.Join(dir, "svc", "2026-10-01-md-only")
	os.MkdirAll(md, 0o755)
	os.WriteFile(filepath.Join(md, "report.md"), []byte("---\ntitle: MD only\n---\n"), 0o644)

	var list ReportsResponse
	if code := eng.do("GET", "/api/reports", nil, &list); code != 200 || len(list.Reports) != 2 || !list.CanReadMD ||
		list.Reports[0].Title != "COBE" || !list.Reports[0].HasMD || strings.Join(list.Reports[0].Tags, "|") != "a|b" {
		t.Fatalf("engineer list = %d %+v", code, list)
	}
	// QA sees only reports with a stakeholder HTML, and never the md.
	if code := qa.do("GET", "/api/reports", nil, &list); code != 200 || len(list.Reports) != 1 || list.CanReadMD || list.Reports[0].HasMD {
		t.Fatalf("qa list = %d %+v", code, list)
	}

	get := func(c *client, path string) (*http.Response, string) {
		t.Helper()
		resp, err := c.hc.Get(h.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, string(b)
	}
	resp, body := get(qa, "/api/reports/svc/2026-10-06-cobe/report.html")
	if resp.StatusCode != 200 || !strings.Contains(body, "<script>") || !strings.HasPrefix(resp.Header.Get("Content-Security-Policy"), "sandbox allow-scripts") ||
		resp.Header.Get("X-Frame-Options") != "SAMEORIGIN" || resp.Header.Get("Content-Disposition") != "" {
		t.Errorf("qa html = %d %v", resp.StatusCode, resp.Header)
	}
	if resp, _ := get(qa, "/api/reports/svc/2026-10-06-cobe/report.md"); resp.StatusCode != 403 {
		t.Errorf("qa md = %d", resp.StatusCode)
	}
	if resp, _ := get(qa, "/api/reports/archive.zip"); resp.StatusCode != 403 {
		t.Errorf("qa zip = %d", resp.StatusCode)
	}
	resp, body = get(eng, "/api/reports/svc/2026-10-06-cobe/report.md?download=1")
	if resp.StatusCode != 200 || body != "---\ntitle: COBE\ndate: 2026-10-06\ntags: a, b\n---\n# COBE" ||
		resp.Header.Get("Content-Disposition") != `attachment; filename="2026-10-06-cobe.md"` {
		t.Errorf("eng md = %d %v", resp.StatusCode, resp.Header)
	}
	for _, p := range []string{"/api/reports/svc/%2e%2e/report.md", "/api/reports/%2e%2e/svc/report.md",
		"/api/reports/svc/2026-10-06-cobe/INDEX.md", "/api/reports/svc/nope/report.md", "/api/reports/archive.zip?repo=..", "/api/reports/archive.zip?repo=nope"} {
		if resp, _ := get(eng, p); resp.StatusCode != 404 {
			t.Errorf("%s = %d", p, resp.StatusCode)
		}
	}
	resp, body = get(eng, "/api/reports/archive.zip?repo=svc")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/zip" || !strings.HasPrefix(body, "PK") ||
		resp.Header.Get("Content-Disposition") != `attachment; filename="knowledge-svc.zip"` {
		t.Errorf("zip = %d %v", resp.StatusCode, resp.Header)
	}

	h.api.Cfg.KnowledgeDir = ""
	if code := eng.do("GET", "/api/reports", nil, nil); code != 404 {
		t.Errorf("disabled = %d", code)
	}
}
