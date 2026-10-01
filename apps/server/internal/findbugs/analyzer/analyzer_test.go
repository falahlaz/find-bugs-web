package analyzer

import (
	"context"
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

func TestUserPromptRedacts(t *testing.T) {
	p := UserPrompt("abc-1", "user a@b.com Authorization: Bearer tok")
	if strings.Contains(p, "a@b.com") || strings.Contains(p, "Bearer tok") {
		t.Errorf("prompt not redacted: %s", p)
	}
}

func TestClaudeCode(t *testing.T) {
	bin, _ := filepath.Abs("testdata/fake-claude.sh")
	c := ClaudeCode{Bin: bin, Model: "m", Timeout: 2 * time.Second}
	ctx := context.Background()
	d, err := c.Analyze(ctx, "abc-1", "ERROR 504")
	if err != nil || d.Summary != "ESB timeout" || d.Severity != "high" || d.ErrorSource != "esb" || len(d.RelevantLogs) != 1 {
		t.Fatalf("Analyze = %+v, %v", d, err)
	}
	for _, mode := range []string{"error", "garbage", "timeout"} {
		t.Setenv("FAKE_CLAUDE", mode)
		if _, err := c.Analyze(ctx, "abc-1", "x"); err == nil {
			t.Errorf("mode %s should fail", mode)
		}
	}
}
