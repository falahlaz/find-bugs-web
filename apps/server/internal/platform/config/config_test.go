package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Setenv("GP_PORTAL", "vpn.example.com")
	t.Setenv("SPLUNK_URL", "https://splunk.example.com/")
	t.Setenv("SPLUNK_SSO_DOMAIN", "login.example.com")
	t.Setenv("SPLUNK_SPL_TEMPLATES", `{"prod":"index=a {transaction_id}","dev":"index=b {transaction_id}"}`)
	t.Setenv("GP_REACH_HOSTS", "a:443, b:443,")
	c, err := Load(true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Splunk.URL != "https://splunk.example.com" || strings.Join(c.Splunk.Environments(), ",") != "dev,prod" {
		t.Errorf("splunk = %+v", c.Splunk)
	}
	if len(c.GP.ReachHosts) != 2 || c.QueueMax != 10 || c.WaitingExpiry != 30*time.Minute || c.Location.String() != "Asia/Jakarta" {
		t.Errorf("config = %+v", c)
	}

	t.Setenv("SPLUNK_SPL_TEMPLATES", `{"prod":"index=a"}`)
	t.Setenv("QUEUE_MAX", "x")
	if _, err := Load(true); err == nil || !strings.Contains(err.Error(), "{transaction_id}") || !strings.Contains(err.Error(), "QUEUE_MAX") {
		t.Errorf("err = %v", err)
	}
}

func TestLoadGitLab(t *testing.T) {
	t.Setenv("GP_PORTAL", "vpn.example.com")
	t.Setenv("SPLUNK_URL", "https://splunk.example.com")
	t.Setenv("SPLUNK_SSO_DOMAIN", "login.example.com")
	t.Setenv("SPLUNK_SPL_TEMPLATES", `{"prod":"index=a {transaction_id}"}`)
	c, err := Load(true)
	if err != nil || c.GitLab.Enabled() || c.GitLab.Username != "oauth2" || c.GitLab.Ref != "main" || c.GitLab.Group != "my-telkomsel" ||
		c.GitLab.Model != "claude-opus-5-5" || c.GitLab.TraceTimeout != 8*time.Minute {
		t.Fatalf("defaults = %+v, %v", c.GitLab, err)
	}

	t.Setenv("GITLAB_URL", "https://gitlab.example.com/")
	t.Setenv("GITLAB_TOKEN", "glpat-x")
	t.Setenv("GITLAB_CA_FILE", "/does/not/exist.pem")
	t.Setenv("GITLAB_REPO_MAP", `{"web":"other/web"}`)
	if _, err := Load(true); err == nil || !strings.Contains(err.Error(), "GITLAB_CA_FILE") {
		t.Errorf("missing CA file err = %v", err)
	}
	t.Setenv("GITLAB_SKIP_SSL_VERIFY", "true")
	c, err = Load(true)
	if err != nil || !c.GitLab.Enabled() || c.GitLab.URL != "https://gitlab.example.com" || c.GitLab.CAFile != "" || c.GitLab.RepoMap["web"] != "other/web" {
		t.Fatalf("gitlab = %+v, %v", c.GitLab, err)
	}
	t.Setenv("CODE_TRACE_ENABLED", "false")
	if c, _ := Load(true); c.GitLab.Enabled() {
		t.Error("CODE_TRACE_ENABLED=false should disable tracing")
	}
}
