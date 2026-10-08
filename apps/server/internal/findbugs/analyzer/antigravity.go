package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

// AgyRequiredDeny are the permission rules the Antigravity CLI settings must
// deny before logs are given to it. agy has no per-run tool or settings flag,
// so these live in the global ~/.gemini/antigravity-cli/settings.json: no
// commands, no web or MCP access, no writes, and no reads outside the
// temporary directory holding the logs (agy's search_web tool cannot be
// denied, but only reaches Google).
var AgyRequiredDeny = []string{
	"command(*)", "unsandboxed(*)", "read_url(*)", "execute_url(*)", "mcp(*)", "write_file(*)",
	"read_file(/home/)", "read_file(/root/)", "read_file(/etc/)", "read_file(/proc/)", "read_file(/var/)",
	"read_file(/opt/)", "read_file(/srv/)", "read_file(/run/)", "read_file(/sys/)", "read_file(/dev/)",
	"read_file(/mnt/)", "read_file(/media/)", "read_file(/boot/)", "read_file(/usr/)",
}

// CheckAgyPermissions reports which AgyRequiredDeny rules the settings file
// at path is missing.
func CheckAgyPermissions(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var missing []string
	for _, r := range AgyRequiredDeny {
		if !slices.Contains(s.Permissions.Deny, r) {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s does not deny %s", path, strings.Join(missing, ", "))
	}
	return nil
}

// diagnosisSchema is the JSON schema agy enforces on the answer.
const diagnosisSchema = `{"type":"object","properties":{` +
	`"summary":{"type":"string"},"error_type":{"type":"string"},"failed_component":{"type":"string"},` +
	`"likely_cause":{"type":"string"},"severity":{"type":"string","enum":["low","medium","high","critical"]},` +
	`"suggested_action":{"type":"string"},"relevant_logs":{"type":"array","items":{"type":"string"}},` +
	`"error_source":{"type":"string","enum":["esb","tibco","internal","unknown"]}},` +
	`"required":["summary","error_type","failed_component","likely_cause","severity","suggested_action","relevant_logs","error_source"]}`

// agyTools rewrites the prompts for agy's tools: it reads with view_file and
// does not offer grep to the model.
var agyTools = strings.NewReplacer(
	"You can only use the Read, Grep and Glob tools, and only inside that directory.",
	"Only use the view_file tool, and only on ./logs.txt. Do not run commands, search the web or open URLs.",
	"in chunks with offset/limit if it is large", "in line ranges if it is large",
	"first Grep it for errors", "first look for errors",
	"then Read the events around every match", "then read the events around every match",
	"without the line-number prefix the Read tool adds", "without any line-number prefix the view_file tool adds",
)

// AntigravityPrompt is the whole prompt for agy, which has no system prompt
// override.
func AntigravityPrompt(transactionID string, size int64) string {
	return agyTools.Replace(SystemPrompt + "\n\n" + UserPrompt(transactionID, size))
}

var conversationID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Antigravity runs the Antigravity CLI (agy) in headless print mode, signed
// in with the Google account of the user the server runs as, in an empty
// directory holding only the log file. Tools are limited by the global
// settings (see AgyRequiredDeny) and the terminal sandbox.
type Antigravity struct {
	Bin     string
	Model   string
	Timeout time.Duration
	// StateDir is agy's state directory (~/.gemini/antigravity-cli); the
	// conversation, which holds the logs, is deleted from it after each run.
	StateDir string
}

// Args returns the CLI arguments (exposed for tests).
func (a Antigravity) Args(prompt string) []string {
	return []string{
		"--model", a.Model,
		"--output-format", "json",
		"--json-schema", diagnosisSchema,
		"--sandbox",
		"--disable-slash-commands",
		"--print", prompt,
	}
}

// Analyze implements Analyzer.
func (a Antigravity) Analyze(ctx context.Context, transactionID, logPath string) (Diagnosis, error) {
	ctx, cancel := context.WithTimeout(ctx, a.Timeout)
	defer cancel()
	dir, size, err := stageLogs(logPath)
	if err != nil {
		return Diagnosis{}, err
	}
	defer os.RemoveAll(dir)

	cmd := exec.CommandContext(ctx, a.Bin, a.Args(AntigravityPrompt(transactionID, size))...)
	cmd.Dir = dir
	// Own process group so a timeout kills agy and anything it spawned.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if runErr != nil && ctx.Err() == context.DeadlineExceeded {
		return Diagnosis{}, fmt.Errorf("agy timed out after %s", a.Timeout)
	}
	var out struct {
		ConversationID   string          `json:"conversation_id"`
		Status           string          `json:"status"`
		Response         string          `json:"response"`
		Error            string          `json:"error"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		if runErr != nil {
			return Diagnosis{}, fmt.Errorf("agy failed: %w: %s", runErr, tail(stderr.String()+stdout.String()))
		}
		return Diagnosis{}, fmt.Errorf("agy output is not JSON: %s", tail(stdout.String()))
	}
	a.forget(out.ConversationID)
	if out.Status != "SUCCESS" {
		return Diagnosis{}, fmt.Errorf("agy returned %s: %s", out.Status, tail(out.Error+" "+stderr.String()))
	}
	if runErr != nil {
		return Diagnosis{}, fmt.Errorf("agy failed: %w: %s", runErr, tail(stderr.String()))
	}
	result := out.Response
	if len(out.StructuredOutput) > 0 && string(out.StructuredOutput) != "null" {
		result = string(out.StructuredOutput)
	}
	d, err := Parse(result)
	if err != nil {
		return Diagnosis{}, err
	}
	d.Model = a.Model
	return d, nil
}

// forget deletes the stored conversation, which holds a copy of the logs.
func (a Antigravity) forget(id string) {
	if a.StateDir == "" || !conversationID.MatchString(id) {
		return
	}
	os.Remove(filepath.Join(a.StateDir, "conversations", id+".db"))
	os.RemoveAll(filepath.Join(a.StateDir, "conversations", id))
	os.RemoveAll(filepath.Join(a.StateDir, "brain", id))
}

// Fallback analyzes with Primary and, when it fails, with Secondary, so a
// quota or auth problem on one provider does not fail the job.
type Fallback struct {
	Primary, Secondary Analyzer
	// OnFallback is told why Primary failed (optional).
	OnFallback func(err error)
}

// Analyze implements Analyzer.
func (f Fallback) Analyze(ctx context.Context, transactionID, logPath string) (Diagnosis, error) {
	d, err := f.Primary.Analyze(ctx, transactionID, logPath)
	if err == nil || ctx.Err() != nil {
		return d, err
	}
	if f.OnFallback != nil {
		f.OnFallback(err)
	}
	d, err2 := f.Secondary.Analyze(ctx, transactionID, logPath)
	if err2 != nil {
		return Diagnosis{}, fmt.Errorf("%w; fallback: %w", err, err2)
	}
	return d, nil
}
