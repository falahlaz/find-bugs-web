package analyzer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	d, err := Parse("here:\n```json\n{\"summary\":\"s\",\"severity\":\" High \",\"error_source\":\"weird\"}\n```")
	if err != nil || d.Severity != "high" || d.ErrorSource != "unknown" || d.RelevantLogs == nil {
		t.Fatalf("Parse = %+v, %v", d, err)
	}
	for _, bad := range []string{"", "no json", `{"summary":""}`, `{bad`} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestUserPrompt(t *testing.T) {
	p := UserPrompt("abc-1", 3<<20)
	if !strings.Contains(p, "Transaction ID: abc-1") || !strings.Contains(p, "./logs.txt (3.0 MB)") {
		t.Errorf("prompt = %s", p)
	}
	if !strings.Contains(SystemPrompt, "Bahasa Indonesia") {
		t.Error("system prompt should ask for Bahasa Indonesia")
	}
}

func writeLogs(t *testing.T, s string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "job.log")
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClaudeCode(t *testing.T) {
	bin, _ := filepath.Abs("testdata/fake-claude.sh")
	c := ClaudeCode{Bin: bin, Model: "m", Timeout: 2 * time.Second}
	ctx := context.Background()
	d, err := c.Analyze(ctx, "abc-1", writeLogs(t, "#1 [t] ERROR 504\n"))
	if err != nil || d.Summary != "ESB timeout" || d.Severity != "high" || d.ErrorSource != "esb" || len(d.RelevantLogs) != 1 {
		t.Fatalf("Analyze = %+v, %v", d, err)
	}
	if d.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("Model = %q, want the model from modelUsage", d.Model)
	}
	for _, mode := range []string{"error", "garbage", "timeout"} {
		t.Setenv("FAKE_CLAUDE", mode)
		if _, err := c.Analyze(ctx, "abc-1", writeLogs(t, "x")); err == nil {
			t.Errorf("mode %s should fail", mode)
		}
	}
}

func TestFake(t *testing.T) {
	d, err := Fake{}.Analyze(context.Background(), "abc-1", writeLogs(t, "INFO ok\nERROR boom\n"))
	if err != nil || len(d.RelevantLogs) != 1 || d.RelevantLogs[0] != "ERROR boom" {
		t.Fatalf("Fake = %+v, %v", d, err)
	}
}

func TestParseTrace(t *testing.T) {
	repos := []Repo{{Project: "g/svc", Dir: "/r/g/svc"}}
	tr, err := ParseTrace(`{"status":"FOUND","file":"/r/g/svc/server/a.js","line":3}`, repos)
	if err != nil || tr.Status != "found" || tr.File != "server/a.js" || tr.Project != "g/svc" {
		t.Fatalf("ParseTrace = %+v, %v", tr, err)
	}
	tr, _ = ParseTrace(`{"status":"found","project":"g/svc","file":"/usr/src/app/server/b.js","line":-1}`, repos)
	if tr.File != "server/b.js" || tr.Line != 0 {
		t.Errorf("container path not mapped: %+v", tr)
	}
	if tr, _ := ParseTrace(`{"status":"found","project":"other/x","file":"a.js"}`, repos); tr.Status != "not_found" {
		t.Errorf("unknown project should be not_found: %+v", tr)
	}
	for _, bad := range []string{"", `{"status":"maybe"}`, `{bad`} {
		if _, err := ParseTrace(bad, repos); err == nil {
			t.Errorf("ParseTrace(%q) should fail", bad)
		}
	}
}

func TestClaudeCodeTrace(t *testing.T) {
	bin, _ := filepath.Abs("testdata/fake-claude.sh")
	c := ClaudeCode{Bin: bin, Model: "m", Timeout: 2 * time.Second}
	repo := t.TempDir()
	repos := []Repo{{Project: "grp/svc", Dir: repo, Commit: "abc"}}
	logs := writeLogs(t, "#1 [t] ERROR 504\n")
	// The model points at a file the checkout does not have.
	if tr, err := c.Trace(context.Background(), "abc-1", logs, Diagnosis{Summary: "s"}, repos); err != nil || tr.Status != "not_found" {
		t.Fatalf("Trace to a missing file = %+v, %v", tr, err)
	}
	os.MkdirAll(filepath.Join(repo, "server"), 0o755)
	var code strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&code, "line%d\n", i)
	}
	os.WriteFile(filepath.Join(repo, "server", "a.js"), []byte(code.String()), 0o644)
	tr, err := c.Trace(context.Background(), "abc-1", logs, Diagnosis{Summary: "s"}, repos)
	if err != nil || tr.Status != "found" || tr.File != "server/a.js" || tr.Line != 12 || tr.Model != "m" ||
		!strings.Contains(tr.Snippet, ">12 | line12") || !strings.HasPrefix(tr.Snippet, "  5 | line5") {
		t.Fatalf("Trace = %+v, %v", tr, err)
	}
	if _, err := c.Trace(context.Background(), "abc-1", writeLogs(t, "x"), Diagnosis{}, nil); err == nil {
		t.Error("Trace without repos should fail")
	}
}

func TestSnippet(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.js"), []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "secret"), []byte("x\n"), 0o644)
	os.Symlink(filepath.Join(filepath.Dir(dir), "secret"), filepath.Join(dir, "link"))
	if s, err := Snippet(dir, "a.js", 1, 1); err != nil || s != ">1 | one\n 2 | two" {
		t.Errorf("Snippet = %q, %v", s, err)
	}
	for _, c := range []struct {
		file string
		line int
	}{{"a.js", 4}, {"a.js", 0}, {"../secret", 1}, {"link", 1}, {"nope.js", 1}} {
		if _, err := Snippet(dir, c.file, c.line, 1); err == nil {
			t.Errorf("Snippet(%s:%d) should fail", c.file, c.line)
		}
	}
}
