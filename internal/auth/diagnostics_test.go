package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
)

func diagnosticEntries(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestOAuthCallbackFailureDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name, stage, reason string
		status              int
		fixture             oidcFixture
		wrongState          bool
		tokenStatus         int
		wantError           error
	}{
		{"token status", "oidc_token", "http_status", 502, oidcFixture{}, false, 502, ErrUpstream},
		{"nonce", "oidc_verify", "nonce_mismatch", 0, oidcFixture{claims: map[string]any{"nonce": "private-nonce"}}, false, 0, ErrUnauthorized},
		{"state", "flow_claim", "state_mismatch", 0, oidcFixture{}, true, 0, ErrUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, start := startOIDCFixture(t, tt.fixture)
			if tt.tokenStatus != 0 {
				s.HTTP.Transport = qqTransport(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/token" {
						return &http.Response{StatusCode: tt.tokenStatus, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private-body private-token private-password"))}, nil
					}
					return http.DefaultTransport.RoundTrip(r)
				})
			}
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
			state := start.State
			if tt.wrongState {
				state = "private-state"
			}
			_, err := s.Callback(context.Background(), "moeauth", state, "code", start.Cookie, nil)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("public error=%v, want %v", err, tt.wantError)
			}
			found := false
			flowID := strings.Split(start.Cookie, ".")[0]
			for _, e := range diagnosticEntries(t, &logs) {
				if e["level"] == "WARN" && e["stage"] == tt.stage && e["reason"] == tt.reason && e["provider_id"] == "moeauth" && e["flow_id"] == flowID {
					if _, ok := e["duration_ms"].(float64); !ok {
						t.Fatalf("missing duration: %v", e)
					}
					if !strings.Contains(e["msg"].(string), flowID) {
						t.Fatalf("flow ID absent from message: %v", e)
					}
					if tt.status != 0 && e["upstream_status"] != float64(tt.status) {
						t.Fatalf("status lost: %v", e)
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("missing controlled failure stage=%s reason=%s: %s", tt.stage, tt.reason, logs.String())
			}
			for _, secret := range []string{"private-state", "private-code", "private-nonce", "private-body", "private-token", "private-password", start.Cookie, start.State} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("logged secret %q", secret)
				}
			}
		})
	}
}

func TestOAuthDiscoveryDNSDiagnostics(t *testing.T) {
	s := testAuth(t)
	s.Config.OIDCissuer = "https://issuer.test"
	s.Config.OIDCid = "moeauth"
	s.HTTP.Transport = qqTransport(func(r *http.Request) (*http.Response, error) {
		return nil, &net.DNSError{Err: "private-upstream-detail", Name: "private-name", IsNotFound: true}
	})
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	_, err := s.BeginFlow(context.Background(), "moeauth", "login", nil, "/")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("public error lost: %v", err)
	}
	found := false
	for _, e := range diagnosticEntries(t, &logs) {
		if e["level"] == "WARN" && e["stage"] == "oidc_discovery" && e["reason"] == "dns" && e["upstream_host"] == "issuer.test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing DNS stage/host diagnostic: %s", logs.String())
	}
	if strings.Contains(logs.String(), "private-") {
		t.Fatalf("raw DNS error exposed: %s", logs.String())
	}
}

func TestOIDCUserInfoOutageDiagnosticIsNonfatal(t *testing.T) {
	s, start := startOIDCFixture(t, oidcFixture{claims: map[string]any{"name": "verified-name"}, userInfoStatus: 503, userInfo: "private-body private-token"})
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
	if err != nil || result.Credentials == nil {
		t.Fatalf("enrichment outage rejected login: %v", err)
	}
	var foundFailure, foundCompletion bool
	for _, e := range diagnosticEntries(t, &logs) {
		if e["level"] == "WARN" && e["stage"] == "oidc_userinfo" && e["reason"] == "http_status" && e["upstream_status"] == float64(503) {
			foundFailure = true
		}
		if e["stage"] == "callback" && e["result"] == "completed" && e["user_id"] == result.Credentials.Session.UserID {
			foundCompletion = true
		}
	}
	if !foundFailure || !foundCompletion {
		t.Fatalf("missing nonfatal failure/completion: %s", logs.String())
	}
	if strings.Contains(logs.String(), "private-") {
		t.Fatalf("UserInfo response exposed: %s", logs.String())
	}
}

func TestQQCallbackFailureDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name, body, reason string
		status             int
		want               error
	}{
		{"status", "private-provider-body", "http_status", 503, ErrUpstream},
		{"malformed JSON", "private-provider-body", "invalid_json", 200, ErrUpstream},
		{"rejected token", `{"error":100,"error_description":"private-provider-body"}`, "token_rejected", 200, ErrUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testAuth(t)
			s.Config.QQappID, s.Config.QQsecret = "app", "private-app-secret"
			s.HTTP.Transport = qqTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			start, err := s.BeginFlow(context.Background(), "qq", "login", nil, "/")
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
			_, err = s.Callback(context.Background(), "qq", start.State, "private-code", start.Cookie, nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("public error=%v", err)
			}
			found := false
			for _, e := range diagnosticEntries(t, &logs) {
				if e["stage"] == "qq_token" && e["reason"] == tt.reason {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing QQ failure diagnostic: %s", logs.String())
			}
			for _, secret := range []string{"private-provider-body", "private-code", "private-app-secret", start.Cookie} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("exposed %q", secret)
				}
			}
		})
	}
}

func TestSessionResolveFailureReasons(t *testing.T) {
	for _, tt := range []struct {
		name, update, reason string
		want                 error
	}{
		{"expired", "UPDATE sessions SET expires_at=0", "session_expired", ErrUnauthorized},
		{"revoked", "UPDATE sessions SET revoked_at=1", "session_revoked", ErrUnauthorized},
		{"banned", "UPDATE users SET status='banned'", "account_banned", ErrBanned},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testAuth(t)
			c, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: "test", Subject: "one"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Store.DB.Exec(tt.update); err != nil {
				t.Fatal(err)
			}
			_, err = s.Resolve(context.Background(), c.Token)
			if !errors.Is(err, tt.want) {
				t.Fatalf("public error=%v", err)
			}
			fields := DiagnosticFields(err)
			found := false
			for i := 0; i+1 < len(fields); i += 2 {
				if fields[i] == "reason" && fields[i+1] == tt.reason {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing controlled session reason %s: %v", tt.reason, fields)
			}
		})
	}
}

func TestBindingFailureDiagnosticCarriesRequest(t *testing.T) {
	s := testAuth(t)
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	ctx := WithRequestID(context.Background(), "request-correlation")
	err := s.BindIdentity(ctx, "invalid-session", Identity{Provider: "unknown"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("public validation error changed: %v", err)
	}
	found := false
	for _, e := range diagnosticEntries(t, &logs) {
		if e["stage"] == "identity_bind" && e["reason"] == "invalid_input" && e["request_id"] == "request-correlation" && strings.Contains(e["msg"].(string), "request-correlation") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing binding stage/correlation: %s", logs.String())
	}
}

func TestProfileStorageFailureHasControlledStage(t *testing.T) {
	s := testAuth(t)
	if err := s.Store.DB.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := s.Profile(context.Background(), Session{})
	if err == nil {
		t.Fatal("closed storage unexpectedly read profile")
	}
	found := false
	fields := DiagnosticFields(err)
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "stage" && fields[i+1] == "profile_read" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing safe profile storage stage: %v", fields)
	}
}

func TestQQAvatarFailureDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name, raw, body, stage, reason string
		status                         int
	}{
		{"HTTP status", "https://q.qlogo.cn/private-path?token=private-query", "private-body", "qq_avatar_fetch", "http_status", 502},
		{"invalid image", "https://q.qlogo.cn/private-path?token=private-query", "private-body", "qq_avatar_decode", "image_decode_failed", 200},
		{"size bound", "https://q.qlogo.cn/private-path?token=private-query", strings.Repeat("x", 1024*1024+1), "qq_avatar_fetch", "response_too_large", 200},
		{"untrusted host", "https://untrusted.test/private-path?token=private-query", "private-body", "qq_avatar_fetch", "avatar_url_rejected", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testAuth(t)
			s.HTTP.Transport = qqTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			_, err := s.avatar(context.Background(), tt.raw)
			if err == nil {
				t.Fatal("invalid avatar unexpectedly accepted")
			}
			var stage, reason string
			fields := DiagnosticFields(err)
			for i := 0; i+1 < len(fields); i += 2 {
				if fields[i] == "stage" {
					stage, _ = fields[i+1].(string)
				}
				if fields[i] == "reason" {
					reason, _ = fields[i+1].(string)
				}
			}
			if stage != tt.stage || reason != tt.reason {
				t.Fatalf("avatar failure stage=%s reason=%s, want %s %s", stage, reason, tt.stage, tt.reason)
			}
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("public avatar error leaked provider data")
			}
		})
	}
}
