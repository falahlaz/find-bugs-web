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
	ctx := context.Background()
	repo := t.TempDir()
	repos := []Repo{{Project: "grp/svc", Dir: repo, Commit: "abc", Ref: "main"}}
	sess := Session{ID: NewSessionID(), Dir: filepath.Join(t.TempDir(), "job-1")}
	if err := PrepareSession(sess.Dir, writeLogs(t, "#1 [t] ERROR 504\n")); err != nil {
		t.Fatal(err)
	}
	var progress []string
	onProgress := func(l string) { progress = append(progress, l) }
	// The model points at a file the checkout does not have.
	if tr, err := c.Trace(ctx, sess, "abc-1", Diagnosis{Summary: "s"}, repos, nil); err != nil || tr.Status != "not_found" {
		t.Fatalf("Trace to a missing file = %+v, %v", tr, err)
	}
	os.MkdirAll(filepath.Join(repo, "server"), 0o755)
	var code strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&code, "line%d\n", i)
	}
	os.WriteFile(filepath.Join(repo, "server", "a.js"), []byte(code.String()), 0o644)
	tr, err := c.Trace(ctx, sess, "abc-1", Diagnosis{Summary: "s"}, repos, onProgress)
	if err != nil || tr.Status != "found" || tr.File != "server/a.js" || tr.Line != 12 || tr.Model != "m" ||
		!strings.Contains(tr.Snippet, ">12 | line12") || !strings.HasPrefix(tr.Snippet, "  5 | line5") {
		t.Fatalf("Trace = %+v, %v", tr, err)
	}
	if strings.Join(progress, "|") != `Membaca grp/svc/server/a.js|Mencari "ERROR 504" di logs.txt` {
		t.Fatalf("progress = %q", progress)
	}
	if _, err := c.Trace(ctx, sess, "abc-1", Diagnosis{}, nil, nil); err == nil {
		t.Error("Trace without repos should fail")
	}
	cfg := Repo{Project: "ops/cfg", Dir: t.TempDir(), Commit: "c1", Ref: "dev", Env: "dev", Config: true, Deployed: true}
	if _, err := c.Trace(ctx, sess, "abc-1", Diagnosis{}, []Repo{cfg}, nil); err == nil {
		t.Error("Trace with only the config repo should fail")
	}
	progress = nil
	if tr, err := c.Trace(ctx, sess, "abc-1", Diagnosis{Summary: "s"}, []Repo{repos[0], cfg}, onProgress); err != nil || tr.Status != "found" || tr.Project != "grp/svc" {
		t.Fatalf("Trace with config = %+v, %v", tr, err)
	}
	if !strings.Contains(strings.Join(progress, "|"), "Membaca config dev/generalConfig.json") {
		t.Fatalf("progress with config = %q", progress)
	}

	// Follow-ups resume the session.
	sess.Resume = true
	if a, err := c.Ask(ctx, sess, "kenapa?", "", repos, nil); err != nil || a.Text != "Jawaban: baris 12" || a.Model != "m" {
		t.Fatalf("Ask = %+v, %v", a, err)
	}
	if a, err := c.Ask(ctx, sess, "kenapa?", "dulu: x", repos, nil); err != nil || a.Text != "Jawaban dengan recap" {
		t.Fatalf("Ask with recap = %+v, %v", a, err)
	}
	other := Repo{Project: "grp/svc", Dir: t.TempDir(), Commit: "def", Ref: "dev", Env: "dev"}
	os.MkdirAll(filepath.Join(other.Dir, "server"), 0o755)
	os.WriteFile(filepath.Join(other.Dir, "server", "a.js"), []byte(code.String()), 0o644)
	if tr, err := c.Retrace(ctx, sess, other, "", append(repos, other), nil); err != nil || tr.Status != "found" || tr.Line != 12 {
		t.Fatalf("Retrace = %+v, %v", tr, err)
	}
	t.Setenv("FAKE_CLAUDE", "error")
	if _, err := c.Ask(ctx, sess, "x", "", repos, nil); err == nil || !strings.Contains(err.Error(), "API Error: 401") {
		t.Fatalf("Ask error = %v", err)
	}
}

func TestTraceArgsAndPrompts(t *testing.T) {
	c := ClaudeCode{Model: "m"}
	repos := []Repo{{Project: "g/a", Dir: "/w/a", Commit: "1", Ref: "9.4.1", Env: "production"}, {Project: "g/b", Dir: "/w/b", Commit: "2", Ref: "main"}}
	args := strings.Join(c.TraceArgs(Session{ID: "u1"}, repos), " ")
	if !strings.Contains(args, "--session-id u1") || strings.Contains(args, "--resume") || !strings.HasSuffix(args, "--add-dir /w/a --add-dir /w/b") {
		t.Errorf("new session args = %s", args)
	}
	if args := strings.Join(c.TraceArgs(Session{ID: "u1", Resume: true}, repos), " "); !strings.Contains(args, "--resume u1") || strings.Contains(args, "--session-id") {
		t.Errorf("resume args = %s", args)
	}
	p := TracePrompt("T1", Diagnosis{Summary: "s"}, repos)
	if !strings.Contains(p, "g/a → /w/a (commit 1 from 9.4.1, the version deployed to production)") ||
		!strings.Contains(p, "g/b → /w/b (commit 2 of main, which may differ from the deployed version)") {
		t.Errorf("trace prompt:\n%s", p)
	}
	if p := AskPrompt("q?", "", repos); !strings.Contains(p, "An engineer asks:\n\nq?") || strings.Contains(p, "restarted") || strings.Contains(p, `"status"`) {
		t.Errorf("ask prompt:\n%s", p)
	}
	if p := RetracePrompt(repos[1], "recap", repos); !strings.HasPrefix(p, "This conversation was restarted") || !strings.Contains(p, `set "project" to g/b`) {
		t.Errorf("retrace prompt:\n%s", p)
	}
	if !strings.Contains(p, "Runtime JSON config: not available") {
		t.Errorf("trace prompt without config:\n%s", p)
	}
	cfg := Repo{Project: "ops/cfg", Dir: "/w/cfg@1/json-files", Commit: "c1", Ref: "dev", Env: "dev", Config: true, Deployed: true}
	withCfg := append(repos[:2:2], cfg)
	p = TracePrompt("T1", Diagnosis{Summary: "s"}, withCfg)
	if !strings.Contains(p, "Runtime JSON config (GitLab project → directory of its JSON files, version):\n- ops/cfg → /w/cfg@1/json-files (branch dev at commit c1, the config deployed to the dev ConfigMaps)") ||
		strings.Contains(p, "not available") || strings.Index(p, "g/b → ") > strings.Index(p, "Runtime JSON config") {
		t.Errorf("trace prompt with config:\n%s", p)
	}
	cfg.Deployed = false
	if p := AskPrompt("q?", "", []Repo{repos[0], cfg}); !strings.Contains(p, "the last config commit before the error; its deployment to dev could not be confirmed") {
		t.Errorf("ask prompt with fallback config:\n%s", p)
	}
	now := []Repo{repos[0],
		{Project: "g/a", Dir: "/w/a@9", Commit: "9", Ref: "9.5.0", Env: "production", Now: true},
		{Project: "ops/cfg", Dir: "/w/cfg@2/json-files", Commit: "c2", Ref: "dev", Env: "dev", Config: true, Deployed: true, Now: true},
		{Project: "g/b", Env: "dev", Now: true, Err: "boom"}}
	if p := AskPrompt("q?", "", now); !strings.Contains(p, "Deployed now, checked out for this question (GitLab project → directory, version):\n"+
		"- g/a → /w/a@9 (commit 9 from 9.5.0, the version deployed to production now)\n"+
		"- ops/cfg → /w/cfg@2/json-files (branch dev at commit c2, the config deployed to the dev ConfigMaps now)\n"+
		"- g/b: the version deployed to dev now could not be fetched (boom)") || !strings.Contains(p, "not available") {
		t.Errorf("ask prompt with versions deployed now:\n%s", p)
	}
	if args := strings.Join(c.TraceArgs(Session{ID: "u1"}, now), " "); strings.Count(args, "--add-dir") != 3 {
		t.Errorf("args with an unavailable checkout = %s", args)
	}
	if !strings.Contains(p, "**Penyebab**") || !strings.Contains(p, "**Perbaikan**") {
		t.Error("trace prompt does not ask for a structured explanation")
	}
	if !strings.Contains(TraceSystemPrompt, "for local development only") {
		t.Error("system prompt does not explain the runtime config")
	}
	if strings.Contains(TraceSystemPrompt, "single JSON object only") {
		t.Error("the session-wide system prompt must not force JSON answers")
	}
	if id := NewSessionID(); len(id) != 36 || id[14] != '4' || id == NewSessionID() {
		t.Errorf("NewSessionID = %s", id)
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
