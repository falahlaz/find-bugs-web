package redact

import (
	"strings"
	"testing"
)

func TestSensitive(t *testing.T) {
	in := `Authorization: Bearer abc.def-123 password="hunter2" user=john.doe@example.co.id Authorization: basic Zm9v`
	out := Sensitive(in)
	for _, leak := range []string{"abc.def-123", "hunter2", "john.doe@example.co.id", "Zm9v"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked: %s", leak, out)
		}
	}
	if !strings.Contains(out, "[EMAIL_REDACTED]") {
		t.Errorf("email marker missing: %s", out)
	}
}

func TestLogs(t *testing.T) {
	out := Logs("a\x00b\r\nc\rd", 0)
	if out != "ab\nc\nd" {
		t.Errorf("Logs = %q", out)
	}
	if out := Logs(strings.Repeat("x", 20), 10); !strings.HasPrefix(out, strings.Repeat("x", 10)+"\n... [truncated 10") {
		t.Errorf("truncate = %q", out)
	}
	if got := Tail("héllo", 4); got != "llo" {
		t.Errorf("Tail = %q", got)
	}
}
