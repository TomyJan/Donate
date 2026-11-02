package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAuthFailuresHaveSafeActionableNotices(t *testing.T) {
	for _, tc := range []struct {
		stage, reason  string
		base           error
		code, callback string
	}{
		{"qq_confirm", "invalid_number_format", ErrInvalid, "QQ_NUMBER_INVALID", "verification"},
		{"qq_avatar_compare", "image_mismatch", ErrInvalid, "QQ_VERIFICATION_FAILED", "verification"},
		{"qq_avatar_fetch", "timeout", ErrUpstream, "QQ_VERIFICATION_UNAVAILABLE", "provider_unavailable"},
		{"qq_avatar_decode", "image_decode_failed", ErrUpstream, "QQ_VERIFICATION_UNAVAILABLE", "provider_unavailable"},
		{"qq_confirm", "attempt_limit", ErrInvalid, "AUTH_ATTEMPTS_EXCEEDED", "attempt_limit"},
		{"flow_read", "flow_expired", ErrUnauthorized, "AUTH_FLOW_EXPIRED", "flow_expired"},
		{"flow_read", "invalid_cookie", ErrUnauthorized, "AUTH_FLOW_INVALID", "flow_invalid"},
		{"flow_claim", "state_mismatch", ErrUnauthorized, "AUTH_FLOW_INVALID", "flow_invalid"},
		{"qq_confirm", "binding_session_mismatch", ErrUnauthorized, "AUTH_SESSION_CHANGED", "session_changed"},
		{"callback_validate", "authorization_cancelled", ErrInvalid, "LOGIN_CANCELLED", "cancelled"},
		{"oidc_discovery", "timeout", ErrUpstream, "LOGIN_PROVIDER_UNAVAILABLE", "provider_unavailable"},
		{"oidc_token", "client_rejected", ErrUpstream, "LOGIN_CONFIGURATION_ERROR", "configuration"},
		{"oidc_verify", "nonce_mismatch", ErrUnauthorized, "LOGIN_VERIFICATION_FAILED", "verification"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			code, message, callback := UserError(authFailure(tc.base, tc.stage, tc.reason))
			if code != tc.code || callback != tc.callback || message == "" {
				t.Fatalf("notice=%q %q %q", code, message, callback)
			}
		})
	}
	// Session authentication and infrastructure failures must retain their normal
	// HTTP handling, rather than being disguised as a recoverable OAuth problem.
	for _, err := range []error{ErrUnauthorized, authFailure(ErrUnauthorized, "session_resolve", "session_expired"), errors.New("private-token")} {
		if code, _, _ := UserError(err); code != "" {
			t.Fatalf("misclassified %v: %s", err, code)
		}
	}
}

func TestFlowStatusIdentifiesBindingIntentAndExpiredFlow(t *testing.T) {
	s := testAuth(t)
	s.Config.QQappID = "app"
	c, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: "test", Subject: "bind"})
	if err != nil {
		t.Fatal(err)
	}
	flow, err := s.BeginFlow(context.Background(), "qq", "bind", &c.Session, "/")
	if err != nil {
		t.Fatal(err)
	}
	status, err := s.FlowStatus(context.Background(), flow.Cookie)
	if err != nil || status.Intent != "bind" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	s.Now = func() time.Time { return time.Now().Add(time.Hour) }
	_, err = s.FlowStatus(context.Background(), flow.Cookie)
	if code, _, _ := UserError(err); code != "AUTH_FLOW_EXPIRED" {
		t.Fatalf("expired=%s %v", code, err)
	}
}
