package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// agyDeny are the permission rules of the settings each log analysis runs
// with: no commands, no web or MCP access, no writes, and no reads outside
// the temporary directory holding the logs (agy's search_web tool cannot be
// denied, but only reaches Google). They live in a throwaway HOME, so the
// user's own agy settings (used by Remote Control sessions) stay untouched.
var agyDeny = []string{
	"command(*)", "unsandboxed(*)", "read_url(*)", "execute_url(*)", "mcp(*)", "write_file(*)",
	"read_file(/home/)", "read_file(/root/)", "read_file(/etc/)", "read_file(/proc/)", "read_file(/var/)",
	"read_file(/opt/)", "read_file(/srv/)", "read_file(/run/)", "read_file(/sys/)", "read_file(/dev/)",
	"read_file(/mnt/)", "read_file(/media/)", "read_file(/boot/)", "read_file(/usr/)",
}

// agyAuthFiles are copied from the signed-in state directory into the
// throwaway HOME: the OAuth token and the finished onboarding.
var agyAuthFiles = []string{"antigravity-oauth-token", "jetski_state.pbtxt", "installation_id"}

// agyHome creates a throwaway HOME for one agy run, signed in like the
// state directory at stateDir and locked down by agyDeny. Its conversation,
// which holds the logs, goes when the caller removes it.
func agyHome(stateDir string) (string, error) {
	home, err := os.MkdirTemp("", "fbw-agy-home-")
	if err != nil {
		return "", err
	}
	dst := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		os.RemoveAll(home)
		return "", err
	}
	for _, f := range agyAuthFiles {
		b, err := os.ReadFile(filepath.Join(stateDir, f))
		if err != nil {
			os.RemoveAll(home)
			return "", fmt.Errorf("agy is not signed in (%w)", err)
		}
		if err := os.WriteFile(filepath.Join(dst, f), b, 0o600); err != nil {
			os.RemoveAll(home)
			return "", err
		}
	}
	settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{"deny": agyDeny}})
	if err := os.WriteFile(filepath.Join(dst, "settings.json"), settings, 0o600); err != nil {
		os.RemoveAll(home)
		return "", err
	}
	return home, nil
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

// Antigravity runs the Antigravity CLI (agy) in headless print mode, signed
// in with the Google account of the user the server runs as, in an empty
// directory holding only the log file. Each run gets a throwaway HOME whose
// settings deny every tool but reading the logs (see agyDeny), plus the
// terminal sandbox.
type Antigravity struct {
	Bin     string
	Model   string
	Timeout time.Duration
	// StateDir is the signed-in agy state directory
	// (~/.gemini/antigravity-cli) the token is copied from.
	StateDir string
}

// Args returns the CLI arguments (exposed for tests).
func (a Antigravity) Args(prompt string) []string { return a.args(prompt, diagnosisSchema) }

func (a Antigravity) args(prompt, schema string) []string {
	return []string{
		"--model", a.Model,
		"--output-format", "json",
		"--json-schema", schema,
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
	result, err := a.exec(ctx, dir, a.Args(AntigravityPrompt(transactionID, size)))
	if err != nil {
		return Diagnosis{}, err
	}
	d, err := Parse(result)
	if err != nil {
		return Diagnosis{}, err
	}
	d.Model = a.Model
	return d, nil
}

// exec runs agy with args in dir under a throwaway HOME and returns the
// answer: the structured output when there is one, else the response text.
func (a Antigravity) exec(ctx context.Context, dir string, args []string) (string, error) {
	home, err := agyHome(a.StateDir)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(home)

	cmd := exec.CommandContext(ctx, a.Bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+home)
	// Own process group so a timeout kills agy and anything it spawned.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if runErr != nil && ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("agy timed out after %s", a.Timeout)
	}
	var out struct {
		Status           string          `json:"status"`
		Response         string          `json:"response"`
		Error            string          `json:"error"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		if runErr != nil {
			return "", fmt.Errorf("agy failed: %w: %s", runErr, tail(stderr.String()+stdout.String()))
		}
		return "", fmt.Errorf("agy output is not JSON: %s", tail(stdout.String()))
	}
	if out.Status != "SUCCESS" {
		return "", fmt.Errorf("agy returned %s: %s", out.Status, tail(out.Error+" "+stderr.String()))
	}
	if runErr != nil {
		return "", fmt.Errorf("agy failed: %w: %s", runErr, tail(stderr.String()))
	}
	result := out.Response
	if len(out.StructuredOutput) > 0 && string(out.StructuredOutput) != "null" {
		result = string(out.StructuredOutput)
	}
	return result, nil
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
