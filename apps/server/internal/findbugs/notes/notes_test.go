package notes

import "testing"

func TestComponentKey(t *testing.T) {
	const want = "/scrt/a/esb/v1/fulfillment/order/multi-product/submit"
	for _, tc := range []struct{ in, want string }{
		{"https://tdw.digitalcore.telkomsel.com/preprod-web/scrt/a/esb/v1/fulfillment/order/multi-product/submit", want},
		{"https://other.host/dev-web/scrt/a/esb/v1/fulfillment/order/multi-product/submit?x=1", want},
		{"HTTP://h/scrt/a/esb/v1/fulfillment/order/multi-product/submit/", want},
		{"/api/v2/orders/12345/items", "/api/v2/orders/{id}/items"},
		{"/api/users/3f2b1c4e-1a2b-4c3d-8e9f-0a1b2c3d4e5f", "/api/users/{id}"},
		{"/api/txn/A3012610081844455728580", "/api/txn/{id}"},
		{"/api/v1/product/detail", "/api/v1/product/detail"},
		{"  OrderService.submit  ", "orderservice.submit"},
		{"payment  service", "payment service"},
		{"https://host", "/"},
		{"", ""},
	} {
		if got := ComponentKey(tc.in); got != tc.want {
			t.Errorf("ComponentKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestKeys(t *testing.T) {
	if c, e, ok := Keys("/a", " NullPointerException ", false); !ok || c != "/a" || e != "nullpointerexception" {
		t.Errorf("Keys = %q %q %v", c, e, ok)
	}
	if _, e, ok := Keys("/a", "", true); !ok || e != "*" {
		t.Errorf("any type = %q %v", e, ok)
	}
	if _, _, ok := Keys("", "503", false); ok {
		t.Error("empty component matched")
	}
	if _, _, ok := Keys("/a", "", false); ok {
		t.Error("empty error type matched")
	}
}

func TestErrorTypeKey(t *testing.T) {
	// The inputs are error types the analyzer wrote for the same errors.
	for _, tc := range []struct{ in, want string }{
		{"SYS-UXP-0021", "sys-uxp-0021"},
		{"SYS-UXP-0021: Internal Application Error from RuleValidation - Could not get the Eligible Product Information", "sys-uxp-0021"},
		{"BIZ-UXP-0003", "biz-uxp-0003"},
		{"RV-Failed to get balance information (code: 30RV-0006)", "30rv-0006"},
		{"400", "400"},
		{"HTTP 400 Bad Request", "400"},
		{"403 Forbidden", "403"},
		{"<h1>596 Service Not Found</h1>", "596 service not found"},
		{"400 Bad Request", "400"},
		{"502 Bad Gateway", "502"},
		{"503 Service Unavailable", "503"},
		{"502 Bad Gateway - ESB Internal Error (40000)", "502 esb internal error 40000"},
		{"400 - Request with prohibited character were not allowed (status_code: 20011)", "400 20011"},
		{"400 - Service Provider Error: UPP - NOT AUTHORIZED (error code 3002)", "400 3002"},
		{"400 - BIZ-UXP-0003", "biz-uxp-0003"},
		{"404notfound", "404notfound"},
		{"cache_miss", "cache miss"},
		{"cache miss", "cache miss"},
		{`TypeError: Invalid version. Must be a string. Got type "undefined"`, "typeerror invalid version must be a string got type undefined"},
		{`TypeError: Invalid version. Must be a string. Got type "undefined".`, "typeerror invalid version must be a string got type undefined"},
		{" NullPointerException ", "nullpointerexception"},
		{"java.lang.NullPointerException", "nullpointerexception"},
		{"Gateway Error", "gateway error"},
		{"RV-Failed to get balance information", "rv failed to get balance information"},
		{"4000 items", "4000 items"},
		{"2026-10-09 timeout", "2026 10 09 timeout"},
		{"", ""},
	} {
		if got := ErrorTypeKey(tc.in); got != tc.want {
			t.Errorf("ErrorTypeKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
