package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEngineerNotes(t *testing.T) {
	ctx := context.Background()
	s, now := newStore(t)
	u, _ := s.CreateUser(ctx, "eng", "h", RoleEngineer)
	j, _ := s.EnqueueJob(ctx, NewJob{UserID: u.ID, TransactionID: "T", Environment: "dev", TimeRange: "24h", InputKind: "id"}, Limits{Total: 5, PerUser: 5})

	if _, err := s.FindNote(ctx, "/a/submit", "503"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find on empty err = %v", err)
	}
	in := NoteInput{ComponentKey: "/a/submit", ErrorTypeKey: "503", Component: "https://h/preprod/a/submit", ErrorType: "503", ErrorSource: "tibco",
		Cause: "pod down", QARecommendation: "retry", CauseText: "Pod mati.", QAText: "Coba lagi.", AIVerdict: NoteVerdictOK, SourceJobID: j.ID, UserID: u.ID}
	n, created, err := s.SaveNote(ctx, in)
	if err != nil || !created || n.State != NoteActive || n.CreatedBy != "eng" || n.SourceJobID == nil || *n.SourceJobID != j.ID {
		t.Fatalf("create = %+v %v %v", n, created, err)
	}

	// Same keys update the note instead of adding another.
	*now = now.Add(time.Minute)
	in.QAText = "Coba lagi 5 menit."
	n2, created, err := s.SaveNote(ctx, in)
	if err != nil || created || n2.ID != n.ID || n2.QAText != "Coba lagi 5 menit." || !n2.UpdatedAt.Equal(*now) {
		t.Fatalf("upsert = %+v %v %v", n2, created, err)
	}

	// A component-wide note only matches when no exact one exists.
	any := in
	any.ErrorTypeKey, any.QAText = AnyErrorType, "umum"
	wide, _, err := s.SaveNote(ctx, any)
	if err != nil || wide.ID == n.ID {
		t.Fatalf("wide = %+v %v", wide, err)
	}
	if f, err := s.FindNote(ctx, "/a/submit", "503"); err != nil || f.ID != n.ID {
		t.Fatalf("exact match = %+v %v", f, err)
	}
	if f, err := s.FindNote(ctx, "/a/submit", "500"); err != nil || f.ID != wide.ID {
		t.Fatalf("wide match = %+v %v", f, err)
	}

	// Editing by ID keeps the keys.
	edit := NoteInput{NoteID: n.ID, Cause: "c", QARecommendation: "q", CauseText: "C", QAText: "Q", AIVerdict: NoteVerdictOverridden, UserID: u.ID}
	if n3, _, err := s.SaveNote(ctx, edit); err != nil || n3.ComponentKey != "/a/submit" || n3.AIVerdict != NoteVerdictOverridden {
		t.Fatalf("edit = %+v %v", n3, err)
	}
	vs, err := s.ListNoteVersions(ctx, n.ID)
	if err != nil || len(vs) != 3 || vs[0].QAText != "Q" || vs[0].Username != "eng" {
		t.Fatalf("versions = %+v %v", vs, err)
	}

	if err := s.ArchiveNote(ctx, n.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveNote(ctx, n.ID, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archive twice err = %v", err)
	}
	if _, _, err := s.SaveNote(ctx, edit); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit archived err = %v", err)
	}
	if f, err := s.FindNote(ctx, "/a/submit", "503"); err != nil || f.ID != wide.ID {
		t.Fatalf("after archive = %+v %v", f, err)
	}
	// The archived note's keys are free again.
	if n4, created, err := s.SaveNote(ctx, in); err != nil || !created || n4.ID == n.ID {
		t.Fatalf("recreate = %+v %v %v", n4, created, err)
	}
	active, _ := s.ListNotes(ctx, NoteActive)
	all, _ := s.ListNotes(ctx, "")
	if len(active) != 2 || len(all) != 3 {
		t.Fatalf("active %d all %d", len(active), len(all))
	}
}
