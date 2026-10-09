package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/analyzer"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/jobs"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/notes"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/httpx"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

const (
	// maxNoteChars bounds each answer of a note.
	maxNoteChars = 4000
	// noteReviewTimeout keeps the AI review inside the server's write timeout.
	noteReviewTimeout = 50 * time.Second
)

func (a *API) registerNoteRoutes() {
	eng := []string{engineer}
	a.add(route{method: "POST", path: "/api/notes/check", summary: "AI review of an engineer note before saving", tag: "notes", opID: "checkNote", roles: eng,
		req: NoteCheckRequest{}, resps: map[int]any{200: NoteReviewResponse{}}, h: a.checkNote})
	a.add(route{method: "POST", path: "/api/notes", summary: "Create or update the engineer note for a job's error", tag: "notes", opID: "saveNote", roles: eng,
		req: NoteSaveRequest{}, resps: map[int]any{200: NoteView{}, 201: NoteView{}}, h: a.saveNote})
	a.add(route{method: "GET", path: "/api/notes", summary: "Engineer notes", tag: "notes", opID: "listNotes", roles: eng, query: []string{"state"},
		resps: map[int]any{200: NoteListResponse{}}, h: a.listNotes})
	a.add(route{method: "POST", path: "/api/notes/{id}/archive", summary: "Archive an engineer note", tag: "notes", opID: "archiveNote", roles: eng,
		resps: map[int]any{200: OKResponse{}}, h: a.archiveNote})
	a.add(route{method: "GET", path: "/api/notes/{id}/versions", summary: "Revisions of an engineer note", tag: "notes", opID: "noteVersions", roles: eng,
		resps: map[int]any{200: NoteVersionsResponse{}}, h: a.noteVersions})
}

// noteInvestigation returns the diagnosis of a job a note is written on, or
// writes an error.
func (a *API) noteInvestigation(w http.ResponseWriter, r *http.Request, jobID int64) (store.Investigation, bool) {
	_, err := a.Jobs.Get(r.Context(), identity(r).User, jobID)
	if errors.Is(err, jobs.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not_found", err.Error())
		return store.Investigation{}, false
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return store.Investigation{}, false
	}
	inv, err := a.Store.GetInvestigation(r.Context(), jobID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		httpx.Internal(w, r, err)
		return inv, false
	}
	if _, _, ok := notes.Keys(inv.FailedComponent, inv.ErrorType, false); err != nil || inv.LLMFailed || !ok {
		httpx.Error(w, http.StatusConflict, "no_diagnosis", "Job ini belum punya diagnosis dengan komponen dan error type, jadi rekomendasi tidak bisa dicocokkan.")
		return inv, false
	}
	return inv, true
}

// noteDraft returns what the AI reviews a note against: the job's
// diagnosis, or, when editing a note without a job, the note's error.
func (a *API) noteDraft(w http.ResponseWriter, r *http.Request, jobID, noteID int64) (analyzer.NoteDraft, bool) {
	if jobID == 0 && noteID != 0 {
		n, err := a.Store.GetNote(r.Context(), noteID)
		if errors.Is(err, store.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "not_found", "Rekomendasi tidak ditemukan.")
			return analyzer.NoteDraft{}, false
		}
		if err != nil {
			httpx.Internal(w, r, err)
			return analyzer.NoteDraft{}, false
		}
		return analyzer.NoteDraft{ErrorType: n.ErrorType, FailedComponent: n.Component, ErrorSource: n.ErrorSource}, true
	}
	inv, ok := a.noteInvestigation(w, r, jobID)
	return analyzer.NoteDraft{Summary: inv.Summary, ErrorType: inv.ErrorType, FailedComponent: inv.FailedComponent,
		LikelyCause: inv.LikelyCause, ErrorSource: inv.ErrorSource}, ok
}

// noteAnswers trims and validates the engineer's two answers.
func noteAnswers(w http.ResponseWriter, cause, qa string) (string, string, bool) {
	cause, qa = strings.TrimSpace(cause), strings.TrimSpace(qa)
	if cause == "" || qa == "" {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Penyebab error dan rekomendasi untuk QA wajib diisi.")
		return "", "", false
	}
	if utf8.RuneCountInString(cause) > maxNoteChars || utf8.RuneCountInString(qa) > maxNoteChars {
		httpx.Error(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("Tiap jawaban maksimal %d karakter.", maxNoteChars))
		return "", "", false
	}
	return cause, qa, true
}

func (a *API) checkNote(w http.ResponseWriter, r *http.Request) {
	var req NoteCheckRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	cause, qa, ok := noteAnswers(w, req.Cause, req.QARecommendation)
	if !ok {
		return
	}
	draft, ok := a.noteDraft(w, r, req.JobID, req.NoteID)
	if !ok {
		return
	}
	draft.Cause, draft.QARecommendation = cause, qa
	if a.Notes == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "review_unavailable", "Cek AI tidak aktif di server ini. Kamu tetap bisa menyimpan tanpa cek AI.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), noteReviewTimeout)
	defer cancel()
	rv, err := a.Notes.ReviewNote(ctx, draft)
	if err != nil {
		slog.Warn("note review failed", "job", req.JobID, "err", err)
		httpx.Error(w, http.StatusBadGateway, "review_failed", "AI gagal mengecek jawaban. Coba lagi, atau simpan tanpa cek AI.")
		return
	}
	httpx.JSON(w, http.StatusOK, NoteReviewResponse{Verdict: rv.Verdict, Questions: rv.Questions, Suggestions: rv.Suggestions,
		CauseText: rv.CauseText, QAText: rv.QAText, Model: rv.Model})
}

func (a *API) saveNote(w http.ResponseWriter, r *http.Request) {
	var req NoteSaveRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	cause, qa, ok := noteAnswers(w, req.Cause, req.QARecommendation)
	if !ok {
		return
	}
	if !store.ValidNoteVerdict(req.AIVerdict) {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "aiVerdict harus ok, overridden atau unchecked.")
		return
	}
	causeText, qaText := strings.TrimSpace(req.CauseText), strings.TrimSpace(req.QAText)
	if req.AIVerdict == store.NoteVerdictUnchecked || causeText == "" || qaText == "" {
		causeText, qaText = cause, qa
	}
	if utf8.RuneCountInString(causeText) > maxNoteChars || utf8.RuneCountInString(qaText) > maxNoteChars {
		httpx.Error(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("Tiap jawaban maksimal %d karakter.", maxNoteChars))
		return
	}
	u := identity(r).User
	in := store.NoteInput{NoteID: req.NoteID, Cause: cause, QARecommendation: qa, CauseText: causeText, QAText: qaText,
		AIVerdict: req.AIVerdict, SourceJobID: req.JobID, UserID: u.ID}
	if req.NoteID == 0 {
		inv, ok := a.noteInvestigation(w, r, req.JobID)
		if !ok {
			return
		}
		in.ComponentKey, in.ErrorTypeKey, _ = notes.Keys(inv.FailedComponent, inv.ErrorType, req.AllErrorTypes)
		in.Component, in.ErrorType, in.ErrorSource = inv.FailedComponent, inv.ErrorType, inv.ErrorSource
	}
	n, created, err := a.Store.SaveNote(r.Context(), in)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Rekomendasi tidak ditemukan atau sudah diarsipkan.")
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	action, code := "note.update", http.StatusOK
	if created {
		action, code = "note.create", http.StatusCreated
	}
	a.audit(r, action, "ok", fmt.Sprintf("note #%d (job #%d, %s)", n.ID, req.JobID, req.AIVerdict))
	httpx.JSON(w, code, noteView(u, n))
}

func (a *API) listNotes(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	switch state {
	case "":
		state = store.NoteActive
	case "all":
		state = ""
	case store.NoteActive, store.NoteArchived:
	default:
		httpx.Error(w, http.StatusBadRequest, "bad_request", "state harus active, archived atau all.")
		return
	}
	list, err := a.Store.ListNotes(r.Context(), state)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	errs, err := a.Store.ListDiagnosedErrors(r.Context())
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, NoteListResponse{Notes: noteMatches(list, errs)})
}

// noteMatches counts the diagnosed jobs each active note shows on, matching
// as the job page does: the exact error type first, then the component's
// note for every error type.
func noteMatches(list []store.EngineerNote, errs []store.DiagnosedError) []NoteListItem {
	type key struct{ component, errorType string }
	items := make([]NoteListItem, len(list))
	active := map[key]*NoteListItem{}
	for i, n := range list {
		items[i].EngineerNote = n
		if n.State == store.NoteActive {
			active[key{n.ComponentKey, n.ErrorTypeKey}] = &items[i]
		}
	}
	for _, d := range errs {
		ck, ek, ok := notes.Keys(d.FailedComponent, d.ErrorType, false)
		if !ok {
			continue
		}
		it := active[key{ck, ek}]
		if it == nil {
			it = active[key{ck, store.AnyErrorType}]
		}
		if it == nil {
			continue
		}
		it.MatchedJobs++
		if it.LastMatchedAt == nil || d.CreatedAt.After(*it.LastMatchedAt) {
			at := d.CreatedAt
			it.LastMatchedAt = &at
		}
	}
	return items
}

func (a *API) archiveNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := a.Store.ArchiveNote(r.Context(), id, identity(r).User.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Rekomendasi tidak ditemukan atau sudah diarsipkan.")
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.audit(r, "note.archive", "ok", fmt.Sprintf("note #%d", id))
	httpx.JSON(w, http.StatusOK, OKResponse{OK: true})
}

func (a *API) noteVersions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := a.Store.GetNote(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Rekomendasi tidak ditemukan.")
		return
	} else if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	vs, err := a.Store.ListNoteVersions(r.Context(), id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, NoteVersionsResponse{Versions: vs})
}

// matchNote finds the engineer note for a diagnosis. A lookup failure only
// hides the note: the diagnosis stands on its own.
func (a *API) matchNote(ctx context.Context, u store.User, inv store.Investigation) *NoteView {
	if inv.LLMFailed {
		return nil
	}
	ck, ek, ok := notes.Keys(inv.FailedComponent, inv.ErrorType, false)
	if !ok {
		return nil
	}
	n, err := a.Store.FindNote(ctx, ck, ek)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("match engineer note", "job", inv.JobID, "err", err)
		}
		return nil
	}
	v := noteView(u, n)
	return &v
}

func noteView(u store.User, n store.EngineerNote) NoteView {
	v := NoteView{ID: n.ID, Component: n.Component, ErrorType: n.ErrorType, AllErrorTypes: n.ErrorTypeKey == store.AnyErrorType,
		CauseText: n.CauseText, QAText: n.QAText, AIVerdict: n.AIVerdict, UpdatedBy: n.UpdatedBy, UpdatedAt: n.UpdatedAt}
	if u.Role == store.RoleEngineer {
		v.Cause, v.QARecommendation = n.Cause, n.QARecommendation
	}
	return v
}
