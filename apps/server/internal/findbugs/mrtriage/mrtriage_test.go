package mrtriage

import (
	"context"
	"errors"
	"testing"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
)

type fakeGL struct {
	mrs       []gitlab.MergeRequest
	approvals map[int64]gitlab.Approvals
	ds        []gitlab.Discussion
	merged    []int64
}

func (f *fakeGL) MyMergeRequests(context.Context, string, string) ([]gitlab.MergeRequest, error) {
	return f.mrs, nil
}

func (f *fakeGL) MergeRequest(_ context.Context, _, iid int64) (gitlab.MergeRequest, error) {
	for _, m := range f.mrs {
		if m.IID == iid {
			return m, nil
		}
	}
	return gitlab.MergeRequest{}, gitlab.ErrNotFound
}

func (f *fakeGL) Approvals(_ context.Context, _, iid int64) (gitlab.Approvals, error) {
	a, ok := f.approvals[iid]
	if !ok {
		return a, errors.New("boom")
	}
	return a, nil
}

func (f *fakeGL) Discussions(context.Context, int64, int64) ([]gitlab.Discussion, error) {
	return f.ds, nil
}

func (f *fakeGL) Merge(_ context.Context, _, iid int64) (gitlab.MergeRequest, error) {
	f.merged = append(f.merged, iid)
	return gitlab.MergeRequest{IID: iid, State: "merged"}, nil
}

func mr(iid int64, title string, mod func(*gitlab.MergeRequest)) gitlab.MergeRequest {
	m := gitlab.MergeRequest{IID: iid, ProjectID: 1, Title: title, State: "opened"}
	if mod != nil {
		mod(&m)
	}
	return m
}

func iids(ms []MR) []int64 {
	out := []int64{}
	for _, m := range ms {
		out = append(out, m.IID)
	}
	return out
}

func eq(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTriage(t *testing.T) {
	gl := &fakeGL{
		mrs: []gitlab.MergeRequest{
			mr(1, "A | Falah | ready", nil),
			mr(2, "A | Falah | no approval needed", nil),
			mr(3, "A | Falah | needs approval", nil),
			mr(4, "A | Falah | conflict+comments", func(m *gitlab.MergeRequest) { m.HasConflicts, m.UserNotesCount = true, 1 }),
			mr(5, "A | Falah | draft", func(m *gitlab.MergeRequest) { m.Draft = true }),
			mr(6, "A | Budi | someone else", nil),
			mr(7, "A | Falah | merged", func(m *gitlab.MergeRequest) { m.State = "merged" }),
			mr(8, "A | Falah | approvals unreadable", nil),
		},
		approvals: map[int64]gitlab.Approvals{
			1: {Approved: true, Required: 1}, 2: {Required: 0}, 3: {Required: 1, Left: 1},
			4: {Approved: true, Required: 1}, 5: {Required: 1, Left: 1}, 6: {Approved: true},
		},
	}
	s := &Service{GL: gl, Author: "Falah"}
	tr, err := s.Triage(context.Background(), "all")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ got, want []int64 }{
		"ready":    {iids(tr.Ready), []int64{1, 2}},
		"waiting":  {iids(tr.Waiting), []int64{3, 8}},
		"conflict": {iids(tr.Conflict), []int64{4}},
		"comments": {iids(tr.Comments), []int64{4}},
		"drafts":   {iids(tr.Drafts), []int64{5}},
		"others":   {iids(tr.Others), []int64{7}},
	} {
		if !eq(c.got, c.want) {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
	if tr.Others[0].Approvals != nil {
		t.Errorf("merged MR approvals fetched")
	}
}

func TestUnresolvedComments(t *testing.T) {
	note := func(body string, system, resolvable, resolved bool) gitlab.Note {
		n := gitlab.Note{Body: body, System: system, Resolvable: resolvable, Resolved: resolved}
		n.Author.Name = "Budi"
		return n
	}
	gl := &fakeGL{ds: []gitlab.Discussion{
		{ID: "sys", Notes: []gitlab.Note{note("added 1 commit", true, false, false)}},
		{ID: "done", Notes: []gitlab.Note{note("ok", false, true, true)}},
		{ID: "plain", Notes: []gitlab.Note{note("lgtm", false, false, false)}},
		{ID: "open", Notes: []gitlab.Note{note(" fix this ", false, true, false), note("on it", false, true, false)}},
	}}
	gl.ds[3].Notes[0].Position = &struct {
		NewPath string `json:"new_path"`
		NewLine int    `json:"new_line"`
	}{"a.go", 3}
	cs, err := (&Service{GL: gl, Author: "Falah"}).UnresolvedComments(context.Background(), 1, 1)
	if err != nil || len(cs) != 1 {
		t.Fatalf("comments = %+v, %v", cs, err)
	}
	if c := cs[0]; c.Path != "a.go" || c.Line != 3 || c.Body != "fix this" || c.Replies != 1 || c.Author != "Budi" {
		t.Fatalf("comment = %+v", c)
	}
}

func TestMergeRechecks(t *testing.T) {
	gl := &fakeGL{
		mrs: []gitlab.MergeRequest{
			mr(1, "A | Falah | ready", nil),
			mr(2, "A | Falah | conflict", func(m *gitlab.MergeRequest) { m.HasConflicts = true }),
			mr(3, "A | Falah | unapproved", nil),
			mr(4, "A | Budi | not mine", nil),
			mr(5, "A | Falah | comments", func(m *gitlab.MergeRequest) { m.UserNotesCount = 1 }),
		},
		approvals: map[int64]gitlab.Approvals{1: {Approved: true, Required: 1}, 3: {Required: 2, Left: 2}, 4: {Approved: true}, 5: {Approved: true}},
	}
	s := &Service{GL: gl, Author: "Falah"}
	ctx := context.Background()
	if m, err := s.Merge(ctx, 1, 1); err != nil || m.State != "merged" {
		t.Fatalf("merge ready = %+v, %v", m, err)
	}
	for _, iid := range []int64{2, 3, 4, 5} {
		if _, err := s.Merge(ctx, 1, iid); !errors.Is(err, ErrNotReady) {
			t.Errorf("merge %d err = %v", iid, err)
		}
	}
	if !eq(gl.merged, []int64{1}) {
		t.Fatalf("merged = %v", gl.merged)
	}
}
