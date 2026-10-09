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
