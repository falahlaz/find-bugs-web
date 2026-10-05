package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeployedCommit(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.EscapedPath() != "/api/v4/projects/grp%2Fsvc/deployments" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		queries = append(queries, r.URL.RawQuery)
		q := r.URL.Query()
		if q.Get("environment") == "none" {
			fmt.Fprint(w, `[]`)
			return
		}
		if q.Get("page") == "1" {
			// A full page of other jobs in the environment's history.
			fmt.Fprint(w, "["+strings.TrimSuffix(strings.Repeat(`{"ref":"9.4.1","sha":"bad","deployable":{"name":"verify-eks:production"}},`, 50), ",")+"]")
			return
		}
		fmt.Fprint(w, `[{"ref":"9.4.1","sha":"3ffe5b8a3886c7aee6e6fa87141a38efeaca5aee","deployable":{"name":"deploy-eks:production","status":"success","web_url":"https://g/jobs/1","finished_at":"2026-09-23T23:40:16.702+07:00"}}]`)
	}))
	defer srv.Close()
	c, err := New(Config{URL: srv.URL + "/", Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	before := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	d, err := c.DeployedCommit(ctx, "grp/svc", "production", before)
	if err != nil {
		t.Fatal(err)
	}
	if d.SHA != "3ffe5b8a3886c7aee6e6fa87141a38efeaca5aee" || d.Ref != "9.4.1" || d.JobURL != "https://g/jobs/1" || d.FinishedAt.IsZero() {
		t.Fatalf("deployment = %+v", d)
	}
	if len(queries) != 2 || !strings.Contains(queries[0], "finished_before=2026-10-02T10%3A00%3A00Z") || !strings.Contains(queries[0], "status=success") {
		t.Fatalf("queries = %v", queries)
	}
	if _, err := c.DeployedCommit(ctx, "grp/svc", "none", time.Time{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no deployment err = %v", err)
	}
	if _, err := c.DeployedCommit(ctx, "grp/other", "dev", time.Time{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project err = %v", err)
	}
	bad, _ := New(Config{URL: srv.URL, Token: "wrong"})
	if _, err := bad.DeployedCommit(ctx, "grp/svc", "dev", time.Time{}); err == nil || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("unauthorized err = %v", err)
	}
}

func TestResolveRefAndBranches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/grp%2Fsvc/repository/commits/feature%2Fx":
			fmt.Fprint(w, `{"id":"abc"}`)
		case "/api/v4/projects/grp%2Fsvc/repository/branches":
			if r.URL.Query().Get("search") != "fix" {
				t.Errorf("search = %q", r.URL.RawQuery)
			}
			fmt.Fprint(w, `[{"name":"hotfix/a","commit":{"id":"111"}}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, _ := New(Config{URL: srv.URL, Token: "tok"})
	ctx := context.Background()
	if sha, err := c.ResolveRef(ctx, "grp/svc", "feature/x"); err != nil || sha != "abc" {
		t.Fatalf("ResolveRef = %q, %v", sha, err)
	}
	if _, err := c.ResolveRef(ctx, "grp/svc", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing ref err = %v", err)
	}
	bs, err := c.Branches(ctx, "grp/svc", "fix")
	if err != nil || len(bs) != 1 || bs[0] != (Branch{Name: "hotfix/a", Commit: "111"}) {
		t.Fatalf("Branches = %+v, %v", bs, err)
	}
}

func TestBranchDeployment(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v4/projects/grp%2Fcfg/deployments" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		queries = append(queries, r.URL.RawQuery)
		// Newest first: a consumer redeploy and another branch's configmap
		// deploy come before the one asked for.
		fmt.Fprint(w, `[
			{"ref":"dev","sha":"redeploy","environment":{"name":"dev"},"deployable":{"name":"auto_redeploy_nonprod"}},
			{"ref":"staging","sha":"other","environment":{"name":"staging"},"deployable":{"name":"deploy_configmaps_nonprod"}},
			{"ref":"dev","sha":"ba0a8a5cdc92b94fb3d802864f12c766d69a3d52","environment":{"name":"dev"},"deployable":{"name":"deploy_configmaps_nonprod","web_url":"https://g/jobs/2","finished_at":"2026-10-05T16:54:46.879+07:00"}}
		]`)
	}))
	defer srv.Close()
	c, err := New(Config{URL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	before := time.Date(2026, 10, 5, 9, 59, 24, 0, time.UTC)
	d, err := c.BranchDeployment(ctx, "grp/cfg", "dev", "deploy_configmaps", before)
	if err != nil {
		t.Fatal(err)
	}
	if d.SHA != "ba0a8a5cdc92b94fb3d802864f12c766d69a3d52" || d.Environment != "dev" || d.JobURL != "https://g/jobs/2" || d.FinishedAt.IsZero() {
		t.Fatalf("deployment = %+v", d)
	}
	if len(queries) != 1 || strings.Contains(queries[0], "environment=") || !strings.Contains(queries[0], "finished_before=2026-10-05T09%3A59%3A24Z") {
		t.Fatalf("queries = %v", queries)
	}
	if _, err := c.BranchDeployment(ctx, "grp/cfg", "preprod", "deploy_configmaps", time.Time{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no deployment err = %v", err)
	}
}
