package auth

import "testing"

func TestPaymentReturnPathRetainsOnlyValidLocalOrderLocator(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"/", "/"}, {"/admin", "/admin"},
		{"/?payment=returned&order=68f15e68-143e-4b30-a2a9-87c0df4c5479&trade_status=TRADE_SUCCESS", "/?payment=returned&order=68f15e68-143e-4b30-a2a9-87c0df4c5479"},
		{"https://evil.test/?payment=returned&order=68f15e68-143e-4b30-a2a9-87c0df4c5479", "/"},
		{"//evil.test/?payment=returned&order=68f15e68-143e-4b30-a2a9-87c0df4c5479", "/"},
		{"/?payment=returned&order=not-an-id", "/"},
		{"/?payment=returned&order=68f15e68-143e-4b30-a2a9-87c0df4c5479&order=another", "/"},
	} {
		if got := validReturn(tc.input); got != tc.want {
			t.Errorf("validReturn(%q)=%q want %q", tc.input, got, tc.want)
		}
	}
}
