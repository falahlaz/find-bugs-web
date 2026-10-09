package api

import (
	"context"
	"testing"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

// doneJob submits a job as qa and runs it, returning its path.
func (h *harness) doneJob(qa *client, txn string) (int64, string) {
	h.t.Helper()
	h.splunk.SetLogs(txn, "ERROR boom")
	var sub SubmitJobResponse
	if code := qa.do("POST", "/api/jobs", SubmitJobRequest{Environment: "prod", TimeRange: "24h", Input: txn}, &sub); code != 202 {
		h.t.Fatalf("submit %s = %d", txn, code)
	}
	if _, err := h.wk.Step(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	return sub.Job.ID, "/api/jobs/" + itoa(sub.Job.ID)
}

func TestEngineerNotes(t *testing.T) {
	h := newHarness(t)
	qa, eng := h.login("qa1"), h.login("eng")
	id, path := h.doneJob(qa, "note-1")

	good := NoteCheckRequest{JobID: id, Cause: "Service fake-service kehabisan koneksi DB saat traffic tinggi",
		QARecommendation: "Tunggu 5 menit lalu ulangi transaksi, kalau masih gagal lapor ke engineer"}
	if code := qa.do("POST", "/api/notes/check", good, nil); code != 403 {
		t.Errorf("qa check = %d", code)
	}
	if code := eng.do("POST", "/api/notes/check", NoteCheckRequest{JobID: id, Cause: " ", QARecommendation: "x"}, nil); code != 400 {
		t.Errorf("empty cause = %d", code)
	}
	if code := eng.do("POST", "/api/notes/check", NoteCheckRequest{JobID: 999, Cause: "a", QARecommendation: "b"}, nil); code != 404 {
		t.Errorf("unknown job = %d", code)
	}
	var rv NoteReviewResponse
	if code := eng.do("POST", "/api/notes/check", NoteCheckRequest{JobID: id, Cause: good.Cause, QARecommendation: "cek aja"}, &rv); code != 200 ||
		rv.Verdict != "needs_revision" || len(rv.Questions) == 0 {
		t.Fatalf("vague check = %d %+v", code, rv)
	}
	if code := eng.do("POST", "/api/notes/check", good, &rv); code != 200 || rv.Verdict != "ok" || rv.QAText == "" {
		t.Fatalf("good check = %d %+v", code, rv)
	}

	var view JobView
	qa.do("GET", path, nil, &view)
	if view.Result == nil || view.Result.EngineerNote != nil {
		t.Fatalf("note before save = %+v", view.Result)
	}

	save := NoteSaveRequest{JobID: id, Cause: good.Cause, QARecommendation: good.QARecommendation, CauseText: rv.CauseText, QAText: rv.QAText, AIVerdict: "ok"}
	if code := qa.do("POST", "/api/notes", save, nil); code != 403 {
		t.Errorf("qa save = %d", code)
	}
	var nv NoteView
	if code := eng.do("POST", "/api/notes", NoteSaveRequest{JobID: id, Cause: "a", QARecommendation: "b", AIVerdict: "maybe"}, nil); code != 400 {
		t.Errorf("bad verdict = %d", code)
	}
	if code := eng.do("POST", "/api/notes", save, &nv); code != 201 || nv.QAText != rv.QAText || nv.UpdatedBy != "eng" || nv.Cause != good.Cause {
		t.Fatalf("save = %d %+v", code, nv)
	}

	// The note shows on every job with the same error, to QA without the
	// engineer's original answers.
	_, path2 := h.doneJob(qa, "note-2")
	qa.do("GET", path2, nil, &view)
	if n := view.Result.EngineerNote; n == nil || n.ID != nv.ID || n.QAText != rv.QAText || n.Cause != "" || n.QARecommendation != "" {
		t.Fatalf("qa note = %+v", n)
	}
	eng.do("GET", path2, nil, &view)
	if n := view.Result.EngineerNote; n == nil || n.Cause != good.Cause {
		t.Fatalf("engineer note = %+v", n)
	}

	// Saving again for the same error updates the note; unchecked shows
	// the original answers.
	save.AIVerdict, save.QARecommendation = "unchecked", "Ulangi setelah 10 menit, lapor engineer jika gagal"
	if code := eng.do("POST", "/api/notes", save, &nv); code != 200 || nv.QAText != save.QARecommendation || nv.AIVerdict != "unchecked" {
		t.Fatalf("resave = %d %+v", code, nv)
	}
	// A note is edited by ID without its job (whose diagnosis may be purged).
	if code := eng.do("POST", "/api/notes/check", NoteCheckRequest{NoteID: nv.ID, Cause: good.Cause, QARecommendation: good.QARecommendation}, &rv); code != 200 || rv.Verdict != "ok" {
		t.Fatalf("check by note = %d %+v", code, rv)
	}
	edit := NoteSaveRequest{NoteID: nv.ID, Cause: good.Cause, QARecommendation: good.QARecommendation, CauseText: rv.CauseText, QAText: rv.QAText, AIVerdict: "ok"}
	if code := eng.do("POST", "/api/notes", edit, &nv); code != 200 || nv.QAText != rv.QAText {
		t.Fatalf("edit by note = %d %+v", code, nv)
	}
	if code := eng.do("POST", "/api/notes", NoteSaveRequest{NoteID: 999, Cause: "a", QARecommendation: "b", AIVerdict: "unchecked"}, nil); code != 404 {
		t.Errorf("edit unknown note = %d", code)
	}
	var vs NoteVersionsResponse
	if code := eng.do("GET", "/api/notes/"+itoa(nv.ID)+"/versions", nil, &vs); code != 200 || len(vs.Versions) != 3 {
		t.Fatalf("versions = %d %+v", code, vs)
	}
	var list NoteListResponse
	if eng.do("GET", "/api/notes", nil, &list); len(list.Notes) != 1 || list.Notes[0].ComponentKey != "fake-service" || list.Notes[0].ErrorTypeKey != "fakeerror" {
		t.Fatalf("list = %+v", list)
	}

	if code := eng.do("POST", "/api/notes/"+itoa(nv.ID)+"/archive", nil, nil); code != 200 {
		t.Fatalf("archive = %d", code)
	}
	if code := eng.do("POST", "/api/notes/"+itoa(nv.ID)+"/archive", nil, nil); code != 404 {
		t.Errorf("archive twice = %d", code)
	}
	var after JobView
	qa.do("GET", path, nil, &after)
	if after.Result.EngineerNote != nil {
		t.Errorf("archived note still shown: %+v", after.Result.EngineerNote)
	}
	if eng.do("GET", "/api/notes?state=archived", nil, &list); len(list.Notes) != 1 || list.Notes[0].State != store.NoteArchived {
		t.Errorf("archived list = %+v", list)
	}
	if code := eng.do("GET", "/api/notes?state=x", nil, nil); code != 400 {
		t.Errorf("bad state = %d", code)
	}
}
