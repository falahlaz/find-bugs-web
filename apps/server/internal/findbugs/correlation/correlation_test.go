package correlation

import (
	"reflect"
	"testing"
)

const client = "A300000000000000000000001"

// Shaped like job 14: payload events keyed by the client ID, then one
// "API Request" event per attempt linking it to a fresh backend _id.
var retried = []string{
	`{"level":"info","tags":["Authentication - Request Payload"],"data":{"transactionid":"A300000000000000000000001"}}`,
	`{"level":"info","tags":["API Request"],"data":{"_id":"A3P0000000000000000000001","url":"/payment/fulfillment/v2","status":400,"mobileapptransactionid":"A300000000000000000000001"}}`,
	`{"level":"info","tags":["Authentication - Request Payload"],"data":{"transactionid":"A300000000000000000000001"}}`,
	`{"level":"info","tags":["API Request"],"data":{"_id":"A3P0000000000000000000002","url":"/payment/fulfillment/v2","status":400,"mobileapptransactionid":"A300000000000000000000001"}}`,
}

func TestBackendIDs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		raws     []string
		searched string
		max      int
		want     []string
	}{
		{"every retry is followed, in order", retried, client, 5, []string{"A3P0000000000000000000001", "A3P0000000000000000000002"}},
		{"capped at max", retried, client, 1, []string{"A3P0000000000000000000001"}},
		{"disabled", retried, client, 0, nil},
		{"case-insensitive match", retried, "a300000000000000000000001", 5, []string{"A3P0000000000000000000001", "A3P0000000000000000000002"}},
		{"duplicates collapse", []string{retried[1], retried[1]}, client, 5, []string{"A3P0000000000000000000001"}},
		// Searching the backend id must not hop again (directed link only).
		{"backend id resolves to nothing", retried, "A3P0000000000000000000001", 5, nil},
		{"_id equal to searched is no link", []string{`{"data":{"_id":"X1","transactionid":"X1"}}`}, "X1", 5, nil},
		{"unrelated event", []string{`{"data":{"_id":"B1","mobileapptransactionid":"OTHER"}}`}, client, 5, nil},
		{"malformed id discarded", []string{`{"data":{"_id":"bad id; | delete","mobileapptransactionid":"` + client + `"}}`}, client, 5, nil},
		{"non-string values ignored", []string{`{"data":{"_id":42,"mobileapptransactionid":"` + client + `"}}`}, client, 5, nil},
		{"field names are case-insensitive", []string{`{"data":{"_ID":"B1","MobileAppTransactionId":"` + client + `"}}`}, client, 5, []string{"B1"}},
		{"truncated JSON falls back to regex", []string{`{"data":{"_id": "B1", "mobileapptransactionid": "` + client + `", "ip": "1.2`}, client, 5, []string{"B1"}},
		{"JSON without data has no ids", []string{`{"_id":"B1","mobileapptransactionid":"` + client + `"}`}, client, 5, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := BackendIDs(tc.raws, tc.searched, tc.max); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("BackendIDs = %v, want %v", got, tc.want)
			}
		})
	}
}
