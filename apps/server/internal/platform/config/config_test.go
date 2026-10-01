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
