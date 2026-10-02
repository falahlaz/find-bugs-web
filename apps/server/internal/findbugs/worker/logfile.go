package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/redact"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

// logFilePath is where a job's full (redacted) Splunk result is written.
func logFilePath(dir string, jobID int64) string {
	return filepath.Join(dir, fmt.Sprintf("job-%d.log", jobID))
}

// writeLogFile writes every fetched event to the job's log file (dir 0700,
// file 0600) and returns its path.
func writeLogFile(dir string, job store.Job, res splunk.Result, linked []string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := logFilePath(dir, job.ID)
	if err := os.WriteFile(path, []byte(formatLogFile(job, res, linked)), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// maxLineChars is the longest line written to the log file. Claude Code's Read
// tool cuts longer lines, so they are wrapped instead.
const maxLineChars = 2000

// formatLogFile renders a header and one numbered, redacted block per event,
// oldest first. JSON events are indented and long lines wrapped (after
// redaction, which can lengthen them), so the analyzer can read every byte.
func formatLogFile(job store.Job, res splunk.Result, linked []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Transaction ID: %s\nEnvironment: %s\nTime range: last %s\n", job.TransactionID, job.Environment, job.TimeRange)
	if len(linked) > 0 {
		fmt.Fprintf(&b, "Linked backend IDs: %s\n", strings.Join(linked, ", "))
		b.WriteString("NOTE: the backend service logs this transaction under its own \"_id\" (one per client attempt), linked to the transaction ID by the \"API Request\" event. Events for all these IDs are merged below; the ID difference is expected, not an anomaly.\n")
	}
	fmt.Fprintf(&b, "Events: %d (oldest first)\n", len(res.Events))
	if res.Truncated {
		fmt.Fprintf(&b, "NOTE: the search matched %d events; only the newest %d are included.\n", res.EventCount, len(res.Events))
	}
	b.WriteString("\n")
	for i, ev := range res.Events {
		fmt.Fprintf(&b, "#%d [%s]\n", i+1, ev.Time)
		raw := strings.TrimSpace(ev.Raw)
		var out bytes.Buffer
		if strings.HasPrefix(raw, "{") && json.Indent(&out, []byte(raw), "", "  ") == nil {
			raw = out.String()
		}
		raw = redact.Logs(raw, 0)
		for _, line := range strings.Split(raw, "\n") {
			for len(line) > maxLineChars {
				cut := maxLineChars
				for cut > 0 && !utf8.RuneStart(line[cut]) {
					cut--
				}
				b.WriteString(line[:cut])
				b.WriteString("\n")
				line = line[cut:]
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// PurgeLogFiles deletes job log files in dir last written before cutoff.
func PurgeLogFiles(dir string, cutoff time.Time) (int, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "job-*.log"))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil || !fi.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(p); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
