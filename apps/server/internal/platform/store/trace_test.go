package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTraceSession(t *testing.T) {
	ctx := context.Background()
	s, now := newStore(t)
	u, _ := s.CreateUser(ctx, "eng", "h", RoleEngineer)
	j, err := s.EnqueueJob(ctx, NewJob{UserID: u.ID, TransactionID: "T", Environment: "dev", TimeRange: "24h", InputKind: "id"}, Limits{Total: 5, PerUser: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartTraceTurn(ctx, j.ID, u.ID, TraceKindChat, "q"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("turn without session err = %v", err)
	}
	repo := TraceRepo{Container: "svc", Project: "g/svc", Dir: "/w/g/svc@1", Commit: "1", Ref: "main", RefSource: RefFallback}
	if err := s.SaveTraceSession(ctx, TraceSession{JobID: j.ID, SessionID: "sid", Dir: "/s/1", Repos: []TraceRepo{repo}}); err != nil {
		t.Fatal(err)
	}
	ts, err := s.GetTraceSession(ctx, j.ID)
	if err != nil || ts.State != TraceOpen || ts.Busy || len(ts.Repos) != 1 || ts.Repos[0] != repo || !ts.LastActivityAt.Equal(*now) {
		t.Fatalf("session = %+v, %v", ts, err)
	}

	*now = now.Add(time.Minute)
	id, err := s.StartTraceTurn(ctx, j.ID, u.ID, TraceKindChat, "kenapa?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartTraceTurn(ctx, j.ID, u.ID, TraceKindChat, "lagi"); !errors.Is(err, ErrTraceBusy) {
		t.Fatalf("second turn err = %v", err)
	}
	s.SetTraceRunning(ctx, id)
	for i := 0; i < maxTraceProgress+5; i++ {
		s.AddTraceProgress(ctx, id, "line")
	}
	msgs, _ := s.ListTraceMessages(ctx, j.ID)
	if len(msgs) != 2 || msgs[0].Role != TraceRoleUser || msgs[0].Username != "eng" || msgs[0].Content != "kenapa?" ||
		msgs[1].Status != TraceMsgRunning || len(msgs[1].Progress) != maxTraceProgress {
		t.Fatalf("messages = %+v", msgs)
	}
	repo2 := TraceRepo{Container: "svc", Project: "g/svc", Dir: "/w/g/svc@2", Commit: "2", Ref: "dev", RefSource: RefManual}
	*now = now.Add(time.Minute)
	if err := s.FinishTraceTurn(ctx, j.ID, id, TraceTurnResult{Content: "karena", Model: "m", CodeTrace: &CodeTrace{Status: TraceFound, File: "a.js"},
		Repos: []TraceRepo{repo, repo2}}); err != nil {
		t.Fatal(err)
	}
	ts, _ = s.GetTraceSession(ctx, j.ID)
	msgs, _ = s.ListTraceMessages(ctx, j.ID)
	if ts.Busy || len(ts.Repos) != 2 || !ts.LastActivityAt.Equal(*now) || msgs[1].Status != TraceMsgDone || msgs[1].Content != "karena" ||
		msgs[1].CodeTrace == nil || msgs[1].CodeTrace.File != "a.js" || msgs[1].FinishedAt == nil {
		t.Fatalf("after finish: %+v %+v", ts, msgs[1])
	}
	dirs, _ := s.ActiveTraceDirs(ctx)
	if !dirs["/w/g/svc@1"] || !dirs["/w/g/svc@2"] {
		t.Fatalf("active dirs = %v", dirs)
	}

	// A turn left running by a crash is failed on recovery.
	id2, _ := s.StartTraceTurn(ctx, j.ID, u.ID, TraceKindChat, "x")
	if err := s.RecoverTraceTurns(ctx, "restart"); err != nil {
		t.Fatal(err)
	}
	msgs, _ = s.ListTraceMessages(ctx, j.ID)
	if ts, _ := s.GetTraceSession(ctx, j.ID); ts.Busy || msgs[3].ID != id2 || msgs[3].Status != TraceMsgFailed || msgs[3].Error != "restart" {
		t.Fatalf("recovered: %+v %+v", ts, msgs[3])
	}

	// Close, purge, reopen with a new conversation.
	if err := s.SetTraceState(ctx, j.ID, TraceClosed, ""); err != nil {
		t.Fatal(err)
	}
	if stale, _ := s.StaleTraceSessions(ctx, now.Add(time.Hour)); len(stale) != 1 {
		t.Fatalf("stale = %+v", stale)
	}
	s.SetTraceState(ctx, j.ID, TracePurged, "")
	if dirs, _ := s.ActiveTraceDirs(ctx); len(dirs) != 0 {
		t.Fatalf("purged session dirs still active: %v", dirs)
	}
	s.SetTraceState(ctx, j.ID, TraceClosed, "")
	if ts, _ := s.GetTraceSession(ctx, j.ID); ts.State != TracePurged {
		t.Fatalf("closing a purged session changed it to %s", ts.State)
	}
	s.SetTraceState(ctx, j.ID, TraceOpen, "sid2")
	ts, _ = s.GetTraceSession(ctx, j.ID)
	if ts.State != TraceOpen || ts.SessionID != "sid2" || !ts.Restart {
		t.Fatalf("reopened purged = %+v", ts)
	}
	id3, _ := s.StartTraceTurn(ctx, j.ID, u.ID, TraceKindChat, "y")
	s.FinishTraceTurn(ctx, j.ID, id3, TraceTurnResult{Content: "z", Started: true})
	if ts, _ := s.GetTraceSession(ctx, j.ID); ts.Restart || ts.SessionID != "sid2" {
		t.Fatalf("after restart turn = %+v", ts)
	}
	// Reopening a session that was not purged keeps its conversation.
	s.SetTraceState(ctx, j.ID, TraceClosed, "")
	s.SetTraceState(ctx, j.ID, TraceOpen, "sid3")
	if ts, _ := s.GetTraceSession(ctx, j.ID); ts.SessionID != "sid2" || ts.Restart {
		t.Fatalf("reopened = %+v", ts)
	}
	if err := s.SetTraceState(ctx, 999, TraceClosed, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing session err = %v", err)
	}
}
