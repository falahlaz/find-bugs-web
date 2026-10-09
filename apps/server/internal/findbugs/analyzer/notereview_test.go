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

var draft = NoteDraft{Summary: "TIBCO 503", ErrorType: "503", FailedComponent: "https://h/submit", ErrorSource: "tibco",
	Cause: "Pod TIBCO submit order di preprod sedang down", QARecommendation: "Tunggu 10 menit lalu coba lagi, kalau masih gagal lapor ke tim TIBCO"}

func TestParseNoteReview(t *testing.T) {
	r, err := ParseNoteReview("```json\n" + `{"verdict":"NEEDS_REVISION","questions":["a"," ","b","c","d"],"cause_text":" x ","qa_text":"- y"}` + "\n```")
	if err != nil || r.Verdict != NoteNeedsRevision || len(r.Questions) != 3 || r.Suggestions == nil || r.CauseText != "x" {
		t.Fatalf("parse = %+v, %v", r, err)
	}
	for _, bad := range []string{"nope", `{"verdict":"maybe","cause_text":"x","qa_text":"y"}`, `{"verdict":"ok","cause_text":"","qa_text":"y"}`} {
		if _, err := ParseNoteReview(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	p := NotePrompt(draft)
	if !strings.Contains(p, "<engineer_cause>\nPod TIBCO") || !strings.Contains(p, "- failed_component: https://h/submit") {
		t.Errorf("prompt = %s", p)
	}
}

func TestClaudeCodeReviewNote(t *testing.T) {
	// The fake proves no tools are enabled, the directory is empty and the
	// note arrived on stdin.
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte(`#!/bin/sh
input=$(cat)
case "$*" in *"--tools  --permission-mode dontAsk "*"--system-prompt You review a note"*) ;; *) echo "flags missing" >&2; exit 3 ;; esac
[ -z "$(ls -A)" ] || { echo "dir not empty" >&2; exit 4; }
case "$input" in *"<engineer_cause>"*) ;; *) echo "note missing" >&2; exit 5 ;; esac
echo '{"type":"result","is_error":false,"result":"{\"verdict\":\"ok\",\"questions\":[],\"suggestions\":[],\"cause_text\":\"Pod mati.\",\"qa_text\":\"- Coba lagi.\"}","modelUsage":{"m1":{}}}'
`), 0o755)
	c := ClaudeCode{Bin: bin, Model: "m", Timeout: 2 * time.Second}
	r, err := c.ReviewNote(context.Background(), draft)
	if err != nil || r.Verdict != NoteOK || r.QAText != "- Coba lagi." || r.Model != "m1" {
		t.Fatalf("ReviewNote = %+v, %v", r, err)
	}
}

func TestAntigravityReviewNote(t *testing.T) {
	bin, _ := filepath.Abs("testdata/fake-agy.sh")
	state := t.TempDir()
	for _, f := range agyAuthFiles {
		os.WriteFile(filepath.Join(state, f), []byte("x-"+f), 0o600)
	}
	a := Antigravity{Bin: bin, Model: "gemini-x", Timeout: 2 * time.Second, StateDir: state}
	r, err := a.ReviewNote(context.Background(), draft)
	if err != nil || r.Verdict != NoteOK || r.CauseText != "Pod TIBCO mati." || r.Model != "gemini-x" {
		t.Fatalf("ReviewNote = %+v, %v", r, err)
	}
}

func TestFakeAndFallbackReviewNote(t *testing.T) {
	ctx := context.Background()
	if r, err := (Fake{}).ReviewNote(ctx, draft); err != nil || r.Verdict != NoteOK || r.QAText == "" {
		t.Fatalf("good note = %+v, %v", r, err)
	}
	vague := draft
	vague.QARecommendation = "cek aja servicenya"
	if r, _ := (Fake{}).ReviewNote(ctx, vague); r.Verdict != NoteNeedsRevision || len(r.Questions) == 0 {
		t.Fatalf("vague note = %+v", r)
	}
	f := Fallback{Primary: Fake{Err: fmt.Errorf("quota")}, Secondary: Fake{}}
	if r, err := f.ReviewNote(ctx, draft); err != nil || r.Model != "fake" {
		t.Fatalf("fallback = %+v, %v", r, err)
	}
}
