package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func avatarPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestOIDCHTTPAvatarUpgrade(t *testing.T) {
	const raw = "http://thirdqq.qlogo.cn/g?b=oidb&k=private-query&s=100"
	const want = "https://thirdqq.qlogo.cn/g?b=oidb&k=private-query&s=100"
	for _, source := range []string{"id_token.picture", "id_token.avatar", "userinfo.picture", "userinfo.avatar"} {
		t.Run(source, func(t *testing.T) {
			fixture := oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: "{\"sub\":\"user-one\"}"}
			parts := strings.Split(source, ".")
			if parts[0] == "id_token" {
				fixture.claims[parts[1]] = raw
			} else {
				body, _ := json.Marshal(map[string]string{"sub": "user-one", parts[1]: raw})
				fixture.userInfo = string(body)
			}
			s, start := startOIDCFixture(t, fixture)
			calls := 0
			s.avatarHTTP = &http.Client{Transport: qqTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != want || r.Method != http.MethodGet {
					t.Errorf("unexpected avatar request: %s %s", r.Method, r.URL)
				}
				for _, header := range []string{"Authorization", "Cookie", "Referer"} {
					if r.Header.Get(header) != "" {
						t.Errorf("avatar request leaked %s", header)
					}
				}
				if _, ok := r.Context().Deadline(); !ok {
					t.Error("avatar probe has no deadline")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(avatarPNG(t)))}, nil
			})}
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
			previous, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: s.Config.OIDCissuer, Subject: "user-one", Nickname: "old", AvatarURL: "https://old.example/avatar.png"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
			if err != nil || result.Credentials == nil {
				t.Fatal("avatar probe must not prevent login", err)
			}
			if result.Credentials.Session.UserID != previous.Session.UserID || result.Credentials.Session.IdentityID != previous.Session.IdentityID {
				t.Fatal("avatar upgrade changed the identity")
			}
			for _, token := range []string{previous.Token, result.Credentials.Token} {
				session, err := s.Resolve(context.Background(), token)
				if err != nil || session.AvatarURL != want {
					t.Fatalf("resolved avatar=%q, want %q: %v", session.AvatarURL, want, err)
				}
				profile, err := s.Profile(context.Background(), session)
				if err != nil || profile.AvatarURL != want || len(profile.Bindings) != 1 || profile.Bindings[0].AvatarURL != want {
					t.Fatalf("profile/binding avatar not upgraded: %+v %v", profile, err)
				}
			}
			var stored string
			if err := s.Store.DB.QueryRow("SELECT avatar_url FROM identities").Scan(&stored); err != nil || stored != want || calls != 1 {
				t.Fatalf("avatar=%q calls=%d: %v", stored, calls, err)
			}
			if strings.Contains(logs.String(), "private-query") || strings.Contains(logs.String(), "test-access-token") {
				t.Fatal("avatar probe leaked a secret into logs")
			}
		})
	}
}

func TestOIDCHTTPAvatarUpgradeFailure(t *testing.T) {
	for _, tt := range []struct {
		name, body, reason string
		status             int
		err                error
	}{
		{name: "HTTP error", status: 503, body: "private-upstream-body", reason: "http_status"},
		{name: "HTML is not an image", status: 200, body: "<html>private-upstream-body</html>", reason: "image_decode_failed"},
		{name: "oversized image", status: 200, body: strings.Repeat("x", 1024*1024+1), reason: "response_too_large"},
		{name: "network error", err: errors.New("private-network-error"), reason: "request_failed"},
		{name: "deadline", err: context.DeadlineExceeded, reason: "timeout"},
		{name: "redirect", status: 302, body: "", reason: "http_status"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const fallback = "https://fallback.example/avatar.png"
			s, start := startOIDCFixture(t, oidcFixture{claims: map[string]any{"name": "name", "picture": "http://cdn.test/avatar.png?token=private-query"}, userInfo: "{\"sub\":\"user-one\",\"picture\":\"https://fallback.example/avatar.png\"}"})
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
			calls := 0
			s.avatarHTTP = &http.Client{Transport: qqTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if tt.err != nil {
					return nil, tt.err
				}
				return &http.Response{StatusCode: tt.status, Header: http.Header{"Location": {"http://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})}
			previous, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: s.Config.OIDCissuer, Subject: "user-one", Nickname: "old", AvatarURL: "https://old.example/avatar.png"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
			if err != nil || result.Credentials == nil || result.Credentials.Session.AvatarURL != fallback || result.Credentials.Session.UserID != previous.Session.UserID {
				t.Fatalf("failed probe broke fallback/login: %+v %v", result, err)
			}
			var stored string
			if err := s.Store.DB.QueryRow("SELECT avatar_url FROM identities").Scan(&stored); err != nil || stored != fallback {
				t.Fatalf("stored avatar=%q, want fallback %q: %v", stored, fallback, err)
			}
			if calls != 1 || !strings.Contains(logs.String(), "https_upgrade_"+tt.reason) {
				t.Fatalf("probe failure not diagnosed: calls=%d log=%s", calls, logs.String())
			}
			for _, secret := range []string{"private-query", "private-upstream-body", "private-network-error", "test-access-token"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("diagnostic leaked %s", secret)
				}
			}
		})
	}
}

func TestOIDCHTTPAvatarUpgradePreservesExistingAndDeduplicates(t *testing.T) {
	const old = "https://old.example/avatar.png"
	for _, tt := range []struct {
		name     string
		userInfo string
		tokenURL string
	}{
		{"no UserInfo endpoint", "", "http://cdn.test/avatar.png"},
		{"accepted UserInfo without avatar", `{"sub":"user-one"}`, "http://cdn.test/avatar.png"},
		{"accepted UserInfo with the same avatar", `{"sub":"user-one","picture":"http://cdn.test/avatar.png"}`, "http://cdn.test/avatar.png"},
		{"accepted UserInfo supplies the only avatar", `{"sub":"user-one","avatar":"http://cdn.test/avatar.png"}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, start := startOIDCFixture(t, oidcFixture{claims: map[string]any{"name": "name", "picture": tt.tokenURL, "avatar": tt.tokenURL}, userInfo: tt.userInfo, noUserInfo: tt.userInfo == ""})
			previous, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: s.Config.OIDCissuer, Subject: "user-one", Nickname: "old", AvatarURL: old})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			s.avatarHTTP = &http.Client{Transport: qqTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, context.DeadlineExceeded })}
			result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
			if err != nil || result.Credentials == nil || result.Credentials.Session.AvatarURL != old || result.Credentials.Session.UserID != previous.Session.UserID || calls != 1 {
				t.Fatalf("existing avatar not preserved or duplicate probes: %+v calls=%d err=%v", result, calls, err)
			}
			var stored string
			if err := s.Store.DB.QueryRow("SELECT avatar_url FROM identities").Scan(&stored); err != nil || stored != old {
				t.Fatalf("stored avatar=%q, want %q: %v", stored, old, err)
			}
			profile, err := s.Profile(context.Background(), result.Credentials.Session)
			if err != nil || profile.AvatarURL != old || len(profile.Bindings) != 1 || profile.Bindings[0].AvatarURL != old {
				t.Fatalf("preserved avatar missing from profile or bindings: %+v %v", profile, err)
			}
		})
	}
}

func TestOIDCEmptyAvatarClaimsClearExistingAvatar(t *testing.T) {
	s, start := startOIDCFixture(t, oidcFixture{claims: map[string]any{"name": "name"}, userInfo: `{"sub":"user-one"}`})
	_, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: s.Config.OIDCissuer, Subject: "user-one", Nickname: "old", AvatarURL: "https://old.example/avatar.png"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
	if err != nil || result.Credentials == nil || result.Credentials.Session.AvatarURL != "" {
		t.Fatalf("explicitly missing avatar was not cleared: %+v %v", result, err)
	}
	var stored string
	if err := s.Store.DB.QueryRow("SELECT avatar_url FROM identities").Scan(&stored); err != nil || stored != "" {
		t.Fatalf("stored avatar=%q, want empty: %v", stored, err)
	}
}
