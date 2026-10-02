package analyzer

import (
	"context"
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
