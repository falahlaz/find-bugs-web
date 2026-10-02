package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	return s, &now
}

func TestUsersAndSessions(t *testing.T) {
	ctx := context.Background()
	s, now := newStore(t)
	u, err := s.CreateUser(ctx, "Alice", "h", RoleQA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "alice", "h", RoleQA); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate err = %v", err)
	}
	if err := s.CreateSession(ctx, Session{TokenHash: "th", UserID: u.ID, CSRFToken: "c", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, got, err := s.GetSession(ctx, "th"); err != nil || got.Username != "Alice" {
		t.Fatalf("GetSession = %+v, %v", got, err)
	}
	inactive := false
	if _, err := s.UpdateUser(ctx, u.ID, UserUpdate{Active: &inactive}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetSession(ctx, "th"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive user session err = %v", err)
	}
	active := true
	s.UpdateUser(ctx, u.ID, UserUpdate{Active: &active})
	*now = now.Add(2 * time.Hour)
	if _, _, err := s.GetSession(ctx, "th"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session err = %v", err)
	}
}

func TestJobLifecycle(t *testing.T) {
	ctx := context.Background()
	s, now := newStore(t)
	a, _ := s.CreateUser(ctx, "a", "h", RoleQA)
	b, _ := s.CreateUser(ctx, "b", "h", RoleEngineer)
	lim := Limits{Total: 4, PerUser: 3}
	nj := func(uid int64) NewJob {
		return NewJob{UserID: uid, TransactionID: "t1", Environment: "prod", TimeRange: "24h", InputKind: "transaction_id"}
	}
	var ids []int64
	for i := 0; i < 3; i++ {
		j, err := s.EnqueueJob(ctx, nj(a.ID), lim)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	if _, err := s.EnqueueJob(ctx, nj(a.ID), lim); !errors.Is(err, ErrUserLimited) {
		t.Fatalf("per-user err = %v", err)
	}
	if _, err := s.EnqueueJob(ctx, nj(b.ID), lim); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueJob(ctx, nj(b.ID), lim); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("total err = %v", err)
	}
	if pos, _ := s.QueuePosition(ctx, ids[2]); pos != 2 {
		t.Errorf("position = %d", pos)
	}
	next, err := s.NextPending(ctx)
	if err != nil || next.ID != ids[0] {
		t.Fatalf("NextPending = %+v, %v", next, err)
	}
	if err := s.SetStatus(ctx, ids[0], PendingStatuses, Transition{Status: StatusCheckingVPN, Stamp: "started_at"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(ctx, ids[0], PendingStatuses, Transition{Status: StatusCancelled}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conditional update err = %v", err)
	}
	s.SetStatus(ctx, ids[0], nil, Transition{Status: StatusDone})
	if err := s.SaveInvestigation(ctx, Investigation{JobID: ids[0], Summary: "boom", RelevantLogs: []string{"l1"}, RawLogSnippet: "raw", Model: "claude-haiku-4-5-20251001"}); err != nil {
		t.Fatal(err)
	}
	if inv, err := s.GetInvestigation(ctx, ids[0]); err != nil || inv.Model != "claude-haiku-4-5-20251001" {
		t.Fatalf("GetInvestigation model = %q, %v", inv.Model, err)
	}
	if j, err := s.FindRecentResult(ctx, "t1", "prod", "24h", now.Add(-24*time.Hour)); err != nil || j.ID != ids[0] || j.FinishedAt == nil {
		t.Fatalf("FindRecentResult = %+v, %v", j, err)
	}

	// waiting → expired after cutoff, measured from the first wait.
	s.SetStatus(ctx, ids[1], nil, Transition{Status: StatusWaitingVPN})
	*now = now.Add(10 * time.Minute)
	s.SetStatus(ctx, ids[1], nil, Transition{Status: StatusWaitingVPN})
	exp, err := s.ExpireWaiting(ctx, now.Add(-5*time.Minute), "vpn")
	if err != nil || len(exp) != 1 || exp[0].ID != ids[1] {
		t.Fatalf("ExpireWaiting = %+v, %v", exp, err)
	}
	s.SetStatus(ctx, ids[2], nil, Transition{Status: StatusSearching})
	if failed, _ := s.FailRunning(ctx, "restart"); len(failed) != 1 || failed[0].ID != ids[2] {
		t.Fatalf("FailRunning = %+v", failed)
	}
	list, _ := s.ListJobs(ctx, JobFilter{UserID: a.ID, TransactionID: "t"})
	if len(list) != 3 || list[0].ID != ids[2] {
		t.Fatalf("ListJobs = %+v", list)
	}

	*now = now.Add(40 * 24 * time.Hour)
	if n, _ := s.PurgeRawLogs(ctx, now.Add(-30*24*time.Hour)); n != 1 {
		t.Errorf("PurgeRawLogs = %d", n)
	}
	inv, _ := s.GetInvestigation(ctx, ids[0])
	if inv.RawLogSnippet != "" || len(inv.RelevantLogs) != 0 || inv.Summary != "boom" {
		t.Errorf("after purge = %+v", inv)
	}
	if n, _ := s.PurgeDiagnoses(ctx, *now); n != 1 {
		t.Errorf("PurgeDiagnoses = %d", n)
	}
	if err := s.AddAudit(ctx, a.ID, "vpn.connect", "ok", ""); err != nil {
		t.Fatal(err)
	}
	if es, _ := s.ListAudit(ctx, 10); len(es) != 1 || es[0].Username != "a" {
		t.Errorf("audit = %+v", es)
	}
}
