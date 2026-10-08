package gitlab

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMergeRequests(t *testing.T) {
	var mergeBody, mergeMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v4/merge_requests":
			if q := r.URL.Query(); q.Get("scope") != "created_by_me" || q.Get("state") != "opened" || q.Get("search") != "Falah" || q.Get("in") != "title" {
				t.Errorf("query = %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `[{"iid":7,"project_id":42,"title":"MTA-1 | Falah | fix","state":"opened","draft":false,"has_conflicts":true,"user_notes_count":2,"source_branch":"feat/x","target_branch":"main","web_url":"https://g/mr/7","references":{"full":"grp/sub/svc!7"}}]`)
		case "/api/v4/projects/42/merge_requests/7/approvals":
			fmt.Fprint(w, `{"approved":true,"approvals_required":1,"approvals_left":0,"approved_by":[{"user":{"name":"Budi"}}]}`)
		case "/api/v4/projects/42/merge_requests/7/discussions":
			fmt.Fprint(w, `[{"id":"a","notes":[{"body":"fix this","resolvable":true,"resolved":false,"author":{"name":"Budi"},"position":{"new_path":"a.go","new_line":3}}]}]`)
		case "/api/v4/projects/42/merge_requests/7/merge":
			b, _ := io.ReadAll(r.Body)
			mergeMethod, mergeBody = r.Method, string(b)
			fmt.Fprint(w, `{"iid":7,"project_id":42,"state":"merged","references":{"full":"grp/sub/svc!7"}}`)
		case "/api/v4/projects/42/merge_requests/8/merge":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"403 Forbidden - insufficient_scope"}`)
		case "/api/v4/projects/42/merge_requests/9/merge":
			w.WriteHeader(http.StatusMethodNotAllowed)
			fmt.Fprint(w, `{"message":"405 Method Not Allowed"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, err := New(Config{URL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mrs, err := c.MyMergeRequests(ctx, "opened", "Falah")
	if err != nil || len(mrs) != 1 {
		t.Fatalf("mrs = %+v, %v", mrs, err)
	}
	if m := mrs[0]; m.ProjectPath() != "grp/sub/svc" || !m.HasConflicts || m.UserNotesCount != 2 || m.SourceBranch != "feat/x" {
		t.Fatalf("mr = %+v", m)
	}
	a, err := c.Approvals(ctx, 42, 7)
	if err != nil || !a.Approved || a.Required != 1 || len(a.ApprovedBy) != 1 || a.ApprovedBy[0] != "Budi" {
		t.Fatalf("approvals = %+v, %v", a, err)
	}
	ds, err := c.Discussions(ctx, 42, 7)
	if err != nil || len(ds) != 1 || ds[0].Notes[0].Position == nil || ds[0].Notes[0].Position.NewPath != "a.go" {
		t.Fatalf("discussions = %+v, %v", ds, err)
	}
	m, err := c.Merge(ctx, 42, 7)
	if err != nil || m.State != "merged" || mergeMethod != http.MethodPut || mergeBody != "should_remove_source_branch=true" {
		t.Fatalf("merge = %+v, %v (%s %q)", m, err, mergeMethod, mergeBody)
	}
	if _, err := c.Merge(ctx, 42, 8); !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "insufficient_scope") {
		t.Fatalf("read-only merge err = %v", err)
	}
	var se *StatusError
	if _, err := c.Merge(ctx, 42, 9); !errors.As(err, &se) || se.Code != 405 {
		t.Fatalf("unmergeable err = %v", err)
	}
	if _, err := c.MergeRequest(ctx, 42, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing MR err = %v", err)
	}
}
