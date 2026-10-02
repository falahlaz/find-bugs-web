// Package gitlab is a small read-only client for the GitLab REST API: which
// commit is deployed to an environment, resolving a branch or tag, and
// searching branches. It backs code tracing, which reads the code that was
// running where the error happened instead of the default branch.
package gitlab

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/redact"
)

// ErrNotFound means GitLab has no such deployment, ref or project.
var ErrNotFound = errors.New("not found")

// DeployJobPrefix names the CI job that rolls a commit out to an
// environment: deploy-eks:<environment>. Other jobs in an environment's
// history (verify-eks, kong-disable, …) do not change what runs there.
const DeployJobPrefix = "deploy-eks:"

// Config configures a Client.
type Config struct {
	URL           string // GitLab base URL, e.g. https://gitlab.example.com
	Token         string // needs read_api
	CAFile        string
	SkipTLSVerify bool
	Timeout       time.Duration // per request
}

// Client calls the GitLab API.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New returns a Client.
func New(cfg Config) (*Client, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.SkipTLSVerify {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // until the internal CA is installed
	} else if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificates in %s", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsCfg
	return &Client{
		base:  strings.TrimRight(cfg.URL, "/"),
		token: cfg.Token,
		http:  &http.Client{Timeout: cfg.Timeout, Transport: tr},
	}, nil
}

// Deployment is a successful roll-out of a commit to an environment.
type Deployment struct {
	Environment string
	Ref         string // branch, tag or refs/merge-requests/N/head it was built from
	SHA         string
	FinishedAt  time.Time
	JobURL      string
}

// deploymentPages caps how far back DeployedCommit looks.
const deploymentPages = 5

// DeployedCommit returns the last successful deploy-eks:<env> deployment of
// project that finished before before (or the latest one if before is
// zero), i.e. the version running in env at that time.
func (c *Client) DeployedCommit(ctx context.Context, project, env string, before time.Time) (Deployment, error) {
	q := url.Values{
		"environment": {env}, "status": {"success"},
		"order_by": {"finished_at"}, "sort": {"desc"}, "per_page": {"50"},
	}
	if !before.IsZero() {
		q.Set("finished_before", before.UTC().Format(time.RFC3339))
	}
	for page := 1; page <= deploymentPages; page++ {
		q.Set("page", fmt.Sprint(page))
		var ds []struct {
			Ref        string `json:"ref"`
			SHA        string `json:"sha"`
			Deployable struct {
				Name       string    `json:"name"`
				Status     string    `json:"status"`
				WebURL     string    `json:"web_url"`
				FinishedAt time.Time `json:"finished_at"`
			} `json:"deployable"`
		}
		if err := c.get(ctx, "/projects/"+url.PathEscape(project)+"/deployments", q, &ds); err != nil {
			return Deployment{}, err
		}
		for _, d := range ds {
			if d.Deployable.Name == DeployJobPrefix+env && d.SHA != "" {
				return Deployment{Environment: env, Ref: d.Ref, SHA: d.SHA, FinishedAt: d.Deployable.FinishedAt, JobURL: d.Deployable.WebURL}, nil
			}
		}
		if len(ds) < 50 {
			break
		}
	}
	return Deployment{}, fmt.Errorf("no successful %s%s deployment of %s: %w", DeployJobPrefix, env, project, ErrNotFound)
}

// ResolveRef returns the commit SHA a branch, tag or commit names.
func (c *Client) ResolveRef(ctx context.Context, project, ref string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := c.get(ctx, "/projects/"+url.PathEscape(project)+"/repository/commits/"+url.PathEscape(ref), nil, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// Branch is a branch and the commit at its tip.
type Branch struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

// Branches returns up to 20 branches of project whose name contains search.
func (c *Client) Branches(ctx context.Context, project, search string) ([]Branch, error) {
	var bs []struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	q := url.Values{"per_page": {"20"}}
	if search != "" {
		q.Set("search", search)
	}
	if err := c.get(ctx, "/projects/"+url.PathEscape(project)+"/repository/branches", q, &bs); err != nil {
		return nil, err
	}
	out := make([]Branch, len(bs))
	for i, b := range bs {
		out[i] = Branch{Name: b.Name, Commit: b.Commit.ID}
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	u := c.base + "/api/v4" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab: %s", redact.Sensitive(err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("gitlab: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("gitlab %s: %w", path, ErrNotFound)
	case resp.StatusCode != http.StatusOK:
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("gitlab %s: HTTP %d: %s", path, resp.StatusCode, redact.Sensitive(msg))
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("gitlab %s: %w", path, err)
	}
	return nil
}
