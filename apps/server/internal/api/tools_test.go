package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/tools"
)

func TestTools(t *testing.T) {
	h := newHarness(t)
	qa := h.login("qa1")

	// Tools disabled → 404, not a crash.
	if code := qa.do("GET", "/api/tools", nil, nil); code != 404 {
		t.Fatalf("disabled list = %d", code)
	}

	ciphers := filepath.Join(t.TempDir(), "ciphers.json")
	os.WriteFile(ciphers, []byte(`{"other":{"packagePassword":"dummy-package-password"}}`), 0o600)
	h.api.Tools = tools.New(ciphers, filepath.Join(t.TempDir(), "missing.pem"), nil)

	var list ToolsResponse
	if code := qa.do("GET", "/api/tools", nil, &list); code != 200 || len(list.Tools) != 4 || list.Tools[0].ID != "payment-deeplink" {
		t.Fatalf("qa list = %d %+v", code, list)
	}

	var res tools.ToolResult
	in := ToolInput{"action": "encrypt", "bid": "00093370"}
	if code := qa.do("POST", "/api/tools/package-id/run", in, &res); code != 200 || len(res.Outputs) != 3 ||
		res.Outputs[1].Value != "c0edec74b3ac7817454b9cb9b6037d11" {
		t.Fatalf("run = %d %+v", code, res)
	}
	if code := qa.do("POST", "/api/tools/package-id/run", ToolInput{"action": "encrypt"}, nil); code != 400 {
		t.Fatalf("missing bid = %d", code)
	}
	if code := qa.do("POST", "/api/tools/hashsign/run", ToolInput{"action": "decrypt", "hashsign": "x"}, nil); code != 422 {
		t.Fatalf("no key = %d", code)
	}
	if code := qa.do("POST", "/api/tools/nope/run", ToolInput{}, nil); code != 404 {
		t.Fatalf("unknown = %d", code)
	}

	entries, _ := h.st.ListAudit(context.Background(), 10)
	if len(entries) != 2 || entries[0].Action != "tool.run" || entries[0].Result != "error" || entries[0].Detail != "hashsign" ||
		entries[1].Result != "ok" || entries[1].Detail != "package-id" || entries[1].Username != "qa1" {
		t.Fatalf("audit = %+v", entries)
	}
}
