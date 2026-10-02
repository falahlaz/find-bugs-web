// Package correlation follows a client transaction ID to the backend IDs some
// services log under (ported from find-bugs-bot scraper/correlation.py on the
// unmerged feat/correlation-id-chaining branch).
//
// QA reads the transaction ID off the request header, but several backend
// services (e.g. service-payment-migration) mint their own `_id` and log the
// rest of the trace under it. The two IDs meet only in the "API Request"
// event, which carries both:
//
//	{"tags": ["API Request"], "data": {
//	    "_id": "A3P1261002094618274858050",                    <- backend id
//	    "mobileapptransactionid": "A301261002094618070858050"  <- header id
//	}}
//
// A client retry produces one such event per attempt, each with its own `_id`,
// so every linked ID is returned, not just one.
package correlation

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/parser"
)

// BackendIDKey is the field holding the backend-generated ID.
const BackendIDKey = "_id"

// LinkedIDKeys are fields that may echo the ID the client sent. `_id` is
// deliberately excluded: an event whose `_id` already equals the searched ID
// proves nothing, and chasing it would walk the correlation backwards.
var LinkedIDKeys = []string{
	"mobileapptransactionid",
	"mobiletransactionid",
	"transactionid",
	"transaction_id",
}

var idFieldRe = regexp.MustCompile(`(?i)"(` + BackendIDKey + `|` + strings.Join(LinkedIDKeys, "|") + `)"\s*:\s*"([^"]*)"`)

func isIDKey(k string) bool {
	return k == BackendIDKey || slices.Contains(LinkedIDKeys, k)
}

// idsInEvent returns the correlation-ID fields of one raw event, keyed by
// lowercased field name. It prefers the JSON `data` object and falls back to a
// regex scan for events that aren't JSON or that Splunk truncated.
func idsInEvent(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	var event map[string]any
	if json.Unmarshal([]byte(raw), &event) == nil {
		data, _ := event["data"].(map[string]any)
		found := map[string]string{}
		for k, v := range data {
			k = strings.ToLower(k)
			if s, ok := v.(string); ok && isIDKey(k) {
				found[k] = s
			}
		}
		return found
	}
	found := map[string]string{}
	for _, m := range idFieldRe.FindAllStringSubmatch(raw, -1) {
		found[strings.ToLower(m[1])] = m[2]
	}
	return found
}

// BackendIDs returns the backend `_id`s linked to searched, in order of first
// appearance, at most max of them (max <= 0 returns nil).
//
// Only a directed link counts: within one event, some non-`_id` field must
// equal searched while `_id` differs. Searching a backend `_id` therefore
// resolves to nothing. IDs come from log text, not user input, so each is
// re-validated before it can reach the SPL template (which has no escaping).
func BackendIDs(raws []string, searched string, max int) []string {
	if max <= 0 || searched == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, raw := range raws {
		ids := idsInEvent(raw)
		backend := ids[BackendIDKey]
		if backend == "" || strings.EqualFold(backend, searched) || seen[backend] {
			continue
		}
		linked := false
		for _, k := range LinkedIDKeys {
			linked = linked || strings.EqualFold(ids[k], searched)
		}
		if !linked {
			continue
		}
		if !parser.ValidTransactionID(backend) {
			slog.Warn("discarding malformed backend id", "txn", searched, "id", truncate(backend, 64))
			continue
		}
		seen[backend] = true
		if len(out) == max {
			slog.Warn("more backend ids than allowed, ignoring the rest", "txn", searched, "max", max, "ignored", backend)
			continue
		}
		out = append(out, backend)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
