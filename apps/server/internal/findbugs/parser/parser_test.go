package parser

import (
	"strings"
	"testing"
)

const hdr = "X-Transaction-ID"

func TestParseTransactionID(t *testing.T) {
	for _, in := range []string{"abc-123", "  a.b_c-1  ", strings.Repeat("a", 128)} {
		r, err := Parse(in, hdr)
		if err != nil || r.Kind != KindTransactionID || r.TransactionID != strings.TrimSpace(in) {
			t.Errorf("Parse(%q) = %+v, %v", in, r, err)
		}
	}
	for _, in := range []string{"", "abc 123", "abc;rm", strings.Repeat("a", 129), `x" OR 1=1`} {
		if _, err := Parse(in, hdr); !IsUserError(err) {
			t.Errorf("Parse(%q) err = %v, want user error", in, err)
		}
	}
}

func TestParseCurl(t *testing.T) {
	cases := []struct {
		in, txn, method, url string
	}{
		{`curl -H "X-Transaction-ID: abc-123" https://api.example.com`, "abc-123", "GET", "https://api.example.com"},
		{"curl 'https://api.example.com/v1' \\\n  -H 'x-transaction-id:  t.1_2' \\\n  --data-raw '{\"a\":1}'", "t.1_2", "POST", "https://api.example.com/v1"},
		{`curl -X put --url https://h/x -H 'Content-Type: application/json' -H "X-Transaction-ID: Z9"`, "Z9", "PUT", "https://h/x"},
		{`curl -H "X-Transaction-ID: q\"1" https://h`, "", "", ""},
	}
	for _, c := range cases {
		r, err := ParseCurl(c.in, hdr)
		if c.txn == "" {
			if !IsUserError(err) {
				t.Errorf("ParseCurl(%q) err = %v, want user error", c.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseCurl(%q): %v", c.in, err)
			continue
		}
		if r.TransactionID != c.txn || r.Method != c.method || r.URL != c.url {
			t.Errorf("ParseCurl(%q) = %+v", c.in, r)
		}
	}
	for _, in := range []string{`curl -H "Other: 1" https://h`, `curl -H "X-Transaction-ID: 1"`, `curl 'unterminated`} {
		if _, err := ParseCurl(in, hdr); !IsUserError(err) {
			t.Errorf("ParseCurl(%q) err = %v", in, err)
		}
	}
	// Real app curl: header name differs per deployment, ANSI-C quoted body.
	app := `curl -X POST -H "Authorization: Bearer x.y.z" -H "TRANSACTIONID: A301261006114235093443870" -H "Content-Type: application/json; charset=UTF-8" --data $'{"amount":6000}' --compressed https://h/dev/api/payment/fulfillment/v2`
	if c, err := ParseCurl(app, "TRANSACTIONID"); err != nil || c.TransactionID != "A301261006114235093443870" || c.Method != "POST" || c.URL != "https://h/dev/api/payment/fulfillment/v2" {
		t.Errorf("ParseCurl(app curl) = %+v, %v", c, err)
	}
	if r, err := Parse(`curl -H "X-SPRINT-IDENTIFIER: sprint-9-sprod-cob-app" `+app[5:], "TRANSACTIONID"); err != nil || r.SprintIdentifier != "sprint-9-sprod-cob-app" {
		t.Errorf("Parse(app curl) sprint = %+v, %v", r, err)
	}
	if r, _ := Parse("abc", hdr); r.SprintIdentifier != "" {
		t.Errorf("Parse(bare id) sprint = %q", r.SprintIdentifier)
	}
	r, err := Parse(`curl -H "X-Transaction-ID: abc" https://h`, hdr)
	if err != nil || r.Kind != KindCurl || r.TransactionID != "abc" {
		t.Errorf("Parse curl = %+v, %v", r, err)
	}
}

func TestShellSplit(t *testing.T) {
	got, err := shellSplit(`a 'b c' "d \"e\" \$f" g\ h`)
	want := []string{"a", "b c", `d "e" $f`, "g h"}
	if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("shellSplit = %q, %v", got, err)
	}
}
