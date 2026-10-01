// Package redact removes secrets and PII from Splunk log text before it is
// stored, shown or sent to the LLM. Ported from find-bugs-bot
// analyzer/llm_analyzer.py (_redact_sensitive, _sanitize_log_lines).
package redact

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	bearerRe   = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9\-_.]+`)
	authRe     = regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|basic|token)\s+)\S+`)
	passwordRe = regexp.MustCompile(`(?i)(password[=:"']\s*)\S+`)
	emailRe    = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)
	ctrlRe     = regexp.MustCompile("[\x00-\x08\x0b\x0c\x0e-\x1f]")
)

// Sensitive masks bearer tokens, authorization headers, passwords and emails.
func Sensitive(s string) string {
	s = bearerRe.ReplaceAllString(s, "${1}[REDACTED]")
	s = authRe.ReplaceAllString(s, "${1}[REDACTED]")
	s = passwordRe.ReplaceAllString(s, "${1}[REDACTED]")
	return emailRe.ReplaceAllString(s, "[EMAIL_REDACTED]")
}

// Logs strips control characters, normalises newlines, redacts secrets and
// truncates to maxChars (0 = no limit).
func Logs(s string, maxChars int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = ctrlRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = Sensitive(s)
	if maxChars > 0 && len(s) > maxChars {
		s = s[:maxChars] + fmt.Sprintf("\n... [truncated %d chars]", len(s)-maxChars)
	}
	return s
}

// Tail returns at most the last n bytes of s, cut on a rune boundary.
func Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8Start(s[0]) {
		s = s[1:]
	}
	return s
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
