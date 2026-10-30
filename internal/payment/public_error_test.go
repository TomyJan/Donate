package payment

import (
	"errors"
	"strings"
	"testing"
)

func TestUserErrorKeepsPaymentFailuresSpecificAndSanitized(t *testing.T) {
	for _, tc := range []struct{ reason, code string }{
		{"channel_unavailable", "PAYMENT_CHANNEL_UNAVAILABLE"},
		{"amount_limit", "PAYMENT_AMOUNT_LIMITED"},
		{"timeout", "PAYMENT_TIMEOUT"},
		{"request_signature", "PAYMENT_CONFIGURATION_ERROR"},
		{"merchant_disabled", "PAYMENT_CONFIGURATION_ERROR"},
		{"response_signature", "PAYMENT_VERIFY_FAILED"},
		{"order_already_paid", "ORDER_ALREADY_PAID"},
		{"upstream_rejected", "PAYMENT_UNAVAILABLE"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			code, message := UserError(&gatewayFailure{reason: tc.reason, base: errors.New("private-key server-response secret")})
			if code != tc.code || message == "" || strings.Contains(message, "secret") {
				t.Fatal(code, message)
			}
		})
	}
}
