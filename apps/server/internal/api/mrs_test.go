package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/gitlab"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/mrtriage"
)

func TestMRs(t *testing.T) {
	h := newHarness(t)
	qa, eng := h.login("qa1"), h.login("eng")
	if code := eng.do("GET", "/api/mrs", nil, nil); code != 404 {
		t.Fatalf("disabled = %d", code)
	}

	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/merge_requests":
			fmt.Fprint(w, `[{"iid":1,"project_id":9,"title":"X | Falah | a","state":"opened","source_branch":"f","target_branch":"main","references":{"full":"grp/svc!1"}},
				{"iid":2,"project_id":9,"title":"X | Falah | b","state":"opened","has_conflicts":true,"source_branch":"g","target_branch":"main","references":{"full":"grp/svc!2"}}]`)
		case "/api/v4/projects/9/merge_requests/1/approvals", "/api/v4/projects/9/merge_requests/2/approvals":
			fmt.Fprint(w, `{"approved":true,"approvals_required":1}`)
		case "/api/v4/projects/9/merge_requests/1":
			fmt.Fprint(w, `{"iid":1,"project_id":9,"title":"X | Falah | a","state":"opened","references":{"full":"grp/svc!1"}}`)
		case "/api/v4/projects/9/merge_requests/2":
			fmt.Fprint(w, `{"iid":2,"project_id":9,"title":"X | Falah | b","state":"opened","has_conflicts":true,"source_branch":"g","target_branch":"main","references":{"full":"grp/svc!2"}}`)
		case "/api/v4/projects/9/merge_requests/1/merge":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"insufficient_scope"}`)
		case "/api/v4/projects/9/merge_requests/1/discussions":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gs.Close()
	gl, err := gitlab.New(gitlab.Config{URL: gs.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	h.api.MRs = &mrtriage.Service{GL: gl, Author: "Falah"}

	if code := qa.do("GET", "/api/mrs", nil, nil); code != 403 {
		t.Fatalf("QA list = %d", code)
	}
	if code := qa.do("POST", "/api/mrs/9/1/merge", nil, nil); code != 403 {
		t.Fatalf("QA merge = %d", code)
	}
	var tr MRTriageResponse
	if code := eng.do("GET", "/api/mrs", nil, &tr); code != 200 || len(tr.Ready) != 1 || len(tr.Conflict) != 1 || tr.Author != "Falah" {
		t.Fatalf("list = %d %+v", code, tr)
	}
	if v := tr.Ready[0]; v.Project != "grp/svc" || v.Approvals == nil || !v.Approvals.Approved || v.Repo != nil {
		t.Fatalf("ready = %+v", v)
	}
	if code := eng.do("GET", "/api/mrs?state=merged", nil, nil); code != 400 {
		t.Fatalf("bad state = %d", code)
	}
	var cs MRCommentsResponse
	if code := eng.do("GET", "/api/mrs/9/1/comments", nil, &cs); code != 200 || cs.Comments == nil {
		t.Fatalf("comments = %d %+v", code, cs)
	}
	if code := eng.do("GET", "/api/mrs/x/1/comments", nil, nil); code != 400 {
		t.Fatalf("bad id = %d", code)
	}
	if code := eng.do("POST", "/api/mrs/9/2/merge", nil, nil); code != 409 {
		t.Fatalf("conflicting merge = %d", code)
	}
	if code := eng.do("POST", "/api/mrs/9/1/merge", nil, nil); code != 403 {
		t.Fatalf("read-only token merge = %d", code)
	}
	es, _ := h.st.ListAudit(context.Background(), 10)
	if len(es) < 2 || es[0].Action != "mr.merge" || es[0].Result != "error" {
		t.Fatalf("audit = %+v", es)
	}
}
