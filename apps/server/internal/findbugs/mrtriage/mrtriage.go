// Package mrtriage sorts the GitLab token owner's merge requests into
// Ready / Conflicts / Comments / Drafts, the way the mymrs CLI
// (tsel-mr-triage) did, and merges the ready ones.
package mrtriage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
)

// GitLab is the subset of gitlab.Client the triage uses.
type GitLab interface {
	MyMergeRequests(ctx context.Context, state, search string) ([]gitlab.MergeRequest, error)
	MergeRequest(ctx context.Context, projectID, iid int64) (gitlab.MergeRequest, error)
	Approvals(ctx context.Context, projectID, iid int64) (gitlab.Approvals, error)
	Discussions(ctx context.Context, projectID, iid int64) ([]gitlab.Discussion, error)
	Merge(ctx context.Context, projectID, iid int64) (gitlab.MergeRequest, error)
}

// Service triages merge requests.
type Service struct {
	GL GitLab
	// Author is the name tag MR titles carry, "| <Author> |"; only those
	// merge requests are shown.
	Author string
}

// MR is a merge request with its approval state. Approvals is nil when it
// could not be read.
type MR struct {
	gitlab.MergeRequest
	Approvals *gitlab.Approvals
}

// Approved reports whether the merge request needs no more approvals.
func (m MR) Approved() bool {
	return m.Approvals != nil && (m.Approvals.Approved || m.Approvals.Required == 0)
}

// Triage is the token owner's merge requests sorted into buckets. An MR
// with both conflicts and comments is in both. Waiting holds the clean
// ones still short of approvals (mymrs did not show them). Others holds
// merged and closed ones when all states were asked for.
type Triage struct {
	Ready, Waiting, Conflict, Comments, Drafts, Others []MR
}

// approvalWorkers bounds the concurrent approval lookups.
const approvalWorkers = 6

// Triage fetches the merge requests in state (opened or all) tagged with
// the author and buckets the open ones.
func (s *Service) Triage(ctx context.Context, state string) (Triage, error) {
	mrs, err := s.GL.MyMergeRequests(ctx, state, s.Author)
	if err != nil {
		return Triage{}, err
	}
	tag := "| " + s.Author + " |"
	var mine []MR
	for _, mr := range mrs {
		if strings.Contains(mr.Title, tag) {
			mine = append(mine, MR{MergeRequest: mr})
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, approvalWorkers)
	for i := range mine {
		if mine[i].State != "opened" {
			continue
		}
		wg.Add(1)
		go func(m *MR) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if a, err := s.GL.Approvals(ctx, m.ProjectID, m.IID); err == nil {
				m.Approvals = &a
			}
		}(&mine[i])
	}
	wg.Wait()
	return bucket(mine), nil
}

func bucket(mrs []MR) Triage {
	t := Triage{Ready: []MR{}, Waiting: []MR{}, Conflict: []MR{}, Comments: []MR{}, Drafts: []MR{}, Others: []MR{}}
	for _, m := range mrs {
		switch {
		case m.State != "opened":
			t.Others = append(t.Others, m)
		case m.Draft:
			t.Drafts = append(t.Drafts, m)
		case m.Approved() && !m.HasConflicts && m.UserNotesCount == 0:
			t.Ready = append(t.Ready, m)
		case !m.HasConflicts && m.UserNotesCount == 0:
			t.Waiting = append(t.Waiting, m)
		default:
			if m.HasConflicts {
				t.Conflict = append(t.Conflict, m)
			}
			if m.UserNotesCount > 0 {
				t.Comments = append(t.Comments, m)
			}
		}
	}
	return t
}

// Comment is the first note of an unresolved thread.
type Comment struct {
	Path    string // "" for a general thread
	Line    int
	Author  string
	Body    string
	Replies int
}

// UnresolvedComments returns the threads with a human note that is not
// resolved; system-only and resolved threads are skipped.
func (s *Service) UnresolvedComments(ctx context.Context, projectID, iid int64) ([]Comment, error) {
	ds, err := s.GL.Discussions(ctx, projectID, iid)
	if err != nil {
		return nil, err
	}
	out := []Comment{}
	for _, d := range ds {
		var human []gitlab.Note
		for _, n := range d.Notes {
			if !n.System {
				human = append(human, n)
			}
		}
		if len(human) == 0 || resolved(human) {
			continue
		}
		first := human[0]
		c := Comment{Author: first.Author.Name, Body: strings.TrimSpace(first.Body), Replies: len(human) - 1}
		if first.Position != nil {
			c.Path, c.Line = first.Position.NewPath, first.Position.NewLine
		}
		out = append(out, c)
	}
	return out, nil
}

// resolved reports whether every resolvable note of a thread is resolved.
// A thread of notes that cannot be resolved (plain MR comments) counts as
// resolved, like mymrs, which read a missing "resolved" as true.
func resolved(notes []gitlab.Note) bool {
	for _, n := range notes {
		if n.Resolvable && !n.Resolved {
			return false
		}
	}
	return true
}

// ErrNotReady means a merge request is no longer ready to merge.
var ErrNotReady = errors.New("MR belum siap di-merge")

// Merge re-reads a merge request and its approvals and merges it only if
// it is still in the Ready bucket: open, not a draft, free of conflicts
// and comments, and approved.
func (s *Service) Merge(ctx context.Context, projectID, iid int64) (gitlab.MergeRequest, error) {
	mr, err := s.GL.MergeRequest(ctx, projectID, iid)
	if err != nil {
		return mr, err
	}
	if !strings.Contains(mr.Title, "| "+s.Author+" |") {
		return mr, fmt.Errorf("%w: bukan MR %s", ErrNotReady, s.Author)
	}
	switch {
	case mr.State != "opened":
		return mr, fmt.Errorf("%w: status %s", ErrNotReady, mr.State)
	case mr.Draft:
		return mr, fmt.Errorf("%w: masih draft", ErrNotReady)
	case mr.HasConflicts:
		return mr, fmt.Errorf("%w: ada konflik", ErrNotReady)
	case mr.UserNotesCount > 0:
		return mr, fmt.Errorf("%w: ada %d komentar", ErrNotReady, mr.UserNotesCount)
	}
	a, err := s.GL.Approvals(ctx, projectID, iid)
	if err != nil {
		return mr, err
	}
	if !a.Approved && a.Required > 0 {
		return mr, fmt.Errorf("%w: butuh %d approval lagi", ErrNotReady, a.Left)
	}
	return s.GL.Merge(ctx, projectID, iid)
}
