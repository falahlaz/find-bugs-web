package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrForbidden means the token may not do what was asked, e.g. merge with a
// read_api token.
var ErrForbidden = errors.New("token GitLab tidak punya izin (butuh scope api, atau branch-nya protected)")

// MergeRequest is the part of a GitLab merge request the MR triage uses.
type MergeRequest struct {
	IID          int64  `json:"iid"`
	ProjectID    int64  `json:"project_id"`
	Title        string `json:"title"`
	State        string `json:"state"` // opened, merged, closed, locked
	Draft        bool   `json:"draft"`
	HasConflicts bool   `json:"has_conflicts"`
	// DetailedMergeStatus is GitLab's mergeability check, e.g. mergeable,
	// conflict, not_approved, or checking/unchecked while has_conflicts is
	// still stale.
	DetailedMergeStatus string    `json:"detailed_merge_status"`
	UserNotesCount      int       `json:"user_notes_count"`
	SourceBranch        string    `json:"source_branch"`
	TargetBranch        string    `json:"target_branch"`
	WebURL              string    `json:"web_url"`
	UpdatedAt           time.Time `json:"updated_at"`
	References          struct {
		Full string `json:"full"` // group/project!iid
	} `json:"references"`
}

// ProjectPath is the group/project the merge request belongs to.
func (mr MergeRequest) ProjectPath() string {
	p, _, _ := strings.Cut(mr.References.Full, "!")
	return p
}

// mrPages caps how many pages of 100 merge requests MyMergeRequests reads.
const mrPages = 5

// MyMergeRequests lists the merge requests created by the token's owner in
// state (opened, merged, closed or all), newest first, up to 500. A
// non-empty search keeps those whose title matches it; the token may be
// shared, so its owner can have far more than one page. GitLab is asked to
// recheck mergeability, so has_conflicts is fresh on the next call.
func (c *Client) MyMergeRequests(ctx context.Context, state, search string) ([]MergeRequest, error) {
	q := url.Values{"scope": {"created_by_me"}, "state": {state}, "per_page": {"100"}, "order_by": {"updated_at"}, "sort": {"desc"},
		"with_merge_status_recheck": {"true"},
	}
	if search != "" {
		q.Set("search", search)
		q.Set("in", "title")
	}
	var out []MergeRequest
	for page := 1; page <= mrPages; page++ {
		q.Set("page", fmt.Sprint(page))
		var mrs []MergeRequest
		if err := c.get(ctx, "/merge_requests", q, &mrs); err != nil {
			return nil, err
		}
		out = append(out, mrs...)
		if len(mrs) < 100 {
			break
		}
	}
	return out, nil
}

func mrPath(projectID, iid int64) string {
	return fmt.Sprintf("/projects/%d/merge_requests/%d", projectID, iid)
}

// MergeRequest returns one merge request.
func (c *Client) MergeRequest(ctx context.Context, projectID, iid int64) (MergeRequest, error) {
	var mr MergeRequest
	err := c.get(ctx, mrPath(projectID, iid), nil, &mr)
	return mr, err
}

// Approvals is the approval state of a merge request.
type Approvals struct {
	Approved   bool
	Required   int
	Left       int
	ApprovedBy []string
}

// Approvals returns who approved a merge request and how many approvals
// are still needed.
func (c *Client) Approvals(ctx context.Context, projectID, iid int64) (Approvals, error) {
	var out struct {
		Approved   bool `json:"approved"`
		Required   int  `json:"approvals_required"`
		Left       int  `json:"approvals_left"`
		ApprovedBy []struct {
			User struct {
				Name string `json:"name"`
			} `json:"user"`
		} `json:"approved_by"`
	}
	if err := c.get(ctx, mrPath(projectID, iid)+"/approvals", nil, &out); err != nil {
		return Approvals{}, err
	}
	a := Approvals{Approved: out.Approved, Required: out.Required, Left: out.Left, ApprovedBy: []string{}}
	for _, u := range out.ApprovedBy {
		a.ApprovedBy = append(a.ApprovedBy, u.User.Name)
	}
	return a, nil
}

// Note is one comment in a discussion.
type Note struct {
	Body     string `json:"body"`
	System   bool   `json:"system"`
	Resolved bool   `json:"resolved"`
	// Resolvable is false for notes that cannot be resolved (then Resolved
	// means nothing).
	Resolvable bool `json:"resolvable"`
	Author     struct {
		Name string `json:"name"`
	} `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	Position  *struct {
		NewPath string `json:"new_path"`
		NewLine int    `json:"new_line"`
	} `json:"position"`
}

// Discussion is a thread of notes on a merge request.
type Discussion struct {
	ID    string `json:"id"`
	Notes []Note `json:"notes"`
}

// discussionPages caps how many pages of 100 threads Discussions reads.
const discussionPages = 5

// Discussions returns the threads on a merge request.
func (c *Client) Discussions(ctx context.Context, projectID, iid int64) ([]Discussion, error) {
	var out []Discussion
	for page := 1; page <= discussionPages; page++ {
		var ds []Discussion
		q := url.Values{"per_page": {"100"}, "page": {fmt.Sprint(page)}}
		if err := c.get(ctx, mrPath(projectID, iid)+"/discussions", q, &ds); err != nil {
			return nil, err
		}
		out = append(out, ds...)
		if len(ds) < 100 {
			break
		}
	}
	return out, nil
}

// Merge merges a merge request and removes its source branch. It needs a
// token with the api scope; a read-only token gets ErrForbidden.
func (c *Client) Merge(ctx context.Context, projectID, iid int64) (MergeRequest, error) {
	var mr MergeRequest
	err := c.do(ctx, http.MethodPut, mrPath(projectID, iid)+"/merge", url.Values{"should_remove_source_branch": {"true"}}, &mr)
	return mr, forbidden(err)
}

// forbidden turns a 401/403 into ErrForbidden.
func forbidden(err error) error {
	var se *StatusError
	if errors.As(err, &se) && (se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden) {
		return fmt.Errorf("%w: %s", ErrForbidden, se.Msg)
	}
	return err
}

// Close closes a merge request without merging it.
func (c *Client) Close(ctx context.Context, projectID, iid int64) (MergeRequest, error) {
	var mr MergeRequest
	err := c.do(ctx, http.MethodPut, mrPath(projectID, iid), url.Values{"state_event": {"close"}}, &mr)
	return mr, forbidden(err)
}

// DeleteBranch deletes a branch of a project. A protected branch, or one
// the token may not push to, gets ErrForbidden; a missing one ErrNotFound.
func (c *Client) DeleteBranch(ctx context.Context, projectID int64, branch string) error {
	err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/projects/%d/repository/branches/%s", projectID, url.PathEscape(branch)), nil, nil)
	return forbidden(err)
}
