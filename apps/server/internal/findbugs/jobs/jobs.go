// Package jobs validates and enqueues investigations and enforces who may
// see or cancel them.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/parser"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

// TimeRanges accepted by the form (Splunk relative time).
var TimeRanges = []string{"24h", "48h"}

// Errors returned by Service.
var (
	ErrNotFound   = errors.New("job tidak ditemukan")
	ErrForbidden  = errors.New("tidak boleh mengakses job ini")
	ErrNotPending = errors.New("job sudah berjalan atau selesai, tidak bisa dibatalkan")
)

// ValidationError is a user-facing input error.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// LimitError explains why the queue rejected a job.
type LimitError struct{ Msg string }

func (e *LimitError) Error() string { return e.Msg }

// Service is the job API used by HTTP handlers.
type Service struct {
	Store        *store.Store
	Environments []string
	Header       string
	Limits       store.Limits
	DedupWindow  time.Duration
	// Wake is signalled after a job is queued or cancelled.
	Wake func()
}

// SubmitRequest is the form input.
type SubmitRequest struct {
	Environment string
	TimeRange   string
	Input       string
	// Force skips the 24h duplicate check ("Jalankan ulang").
	Force bool
}

// Submit validates and enqueues. If a DONE result for the same parameters
// exists inside DedupWindow and Force is false, it returns that job with
// dup=true instead.
func (s *Service) Submit(ctx context.Context, u store.User, req SubmitRequest) (job store.Job, dup bool, err error) {
	if !s.validEnv(req.Environment) {
		return job, false, &ValidationError{Msg: fmt.Sprintf("Environment %q tidak dikenal.", req.Environment)}
	}
	if !contains(TimeRanges, req.TimeRange) {
		return job, false, &ValidationError{Msg: "Time range harus 24h atau 48h."}
	}
	parsed, err := parser.Parse(req.Input, s.Header)
	if err != nil {
		if parser.IsUserError(err) {
			return job, false, &ValidationError{Msg: err.Error()}
		}
		return job, false, err
	}
	if !req.Force && s.DedupWindow > 0 {
		prev, err := s.Store.FindRecentResult(ctx, parsed.TransactionID, req.Environment, req.TimeRange, time.Now().Add(-s.DedupWindow))
		if err == nil {
			return prev, true, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return job, false, err
		}
	}
	job, err = s.Store.EnqueueJob(ctx, store.NewJob{
		UserID: u.ID, TransactionID: parsed.TransactionID, Environment: req.Environment,
		TimeRange: req.TimeRange, InputKind: parsed.Kind,
	}, s.Limits)
	switch {
	case errors.Is(err, store.ErrUserLimited):
		return job, false, &LimitError{Msg: fmt.Sprintf("Kamu sudah punya %d job aktif. Tunggu salah satu selesai dulu.", s.Limits.PerUser)}
	case errors.Is(err, store.ErrQueueFull):
		return job, false, &LimitError{Msg: fmt.Sprintf("Antrean penuh (%d job). Coba lagi beberapa menit lagi.", s.Limits.Total)}
	case err != nil:
		return job, false, err
	}
	if s.Wake != nil {
		s.Wake()
	}
	return job, false, nil
}

func (s *Service) validEnv(env string) bool { return contains(s.Environments, env) }

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

// CanSee reports whether u may view job j.
func CanSee(u store.User, j store.Job) bool {
	return u.Role == store.RoleEngineer || j.UserID == u.ID
}

// Get loads a job the user may see.
func (s *Service) Get(ctx context.Context, u store.User, id int64) (store.Job, error) {
	j, err := s.Store.GetJob(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return j, ErrNotFound
	}
	if err != nil {
		return j, err
	}
	if !CanSee(u, j) {
		// Do not reveal that the job exists.
		return store.Job{}, ErrNotFound
	}
	return j, nil
}

// List returns jobs visible to u; QA users only see their own.
func (s *Service) List(ctx context.Context, u store.User, f store.JobFilter) ([]store.Job, error) {
	if u.Role != store.RoleEngineer && f.UserID != u.ID {
		f.UserID = u.ID
	}
	return s.Store.ListJobs(ctx, f)
}

// Cancel cancels a pending job owned by u (engineers may cancel any).
func (s *Service) Cancel(ctx context.Context, u store.User, id int64) (store.Job, error) {
	j, err := s.Get(ctx, u, id)
	if err != nil {
		return j, err
	}
	if j.UserID != u.ID && u.Role != store.RoleEngineer {
		return j, ErrForbidden
	}
	err = s.Store.SetStatus(ctx, id, store.PendingStatuses, store.Transition{Status: store.StatusCancelled, FailureReason: "Dibatalkan oleh " + u.Username})
	if errors.Is(err, store.ErrConflict) {
		return j, ErrNotPending
	}
	if err != nil {
		return j, err
	}
	if s.Wake != nil {
		s.Wake()
	}
	return s.Store.GetJob(ctx, id)
}

// SortedEnvironments returns a sorted copy (for the form).
func SortedEnvironments(envs []string) []string {
	out := append([]string(nil), envs...)
	sort.Strings(out)
	return out
}
