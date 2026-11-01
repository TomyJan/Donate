package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestOIDCAvatarDiagnosticsDescribeFallbackAndPreservation(t *testing.T) {
	for _, tt := range []struct {
		name                                 string
		fixture                              oidcFixture
		wantAvatar, wantSource, decisionHost string
		wantFallback, wantPreserved          bool
	}{
		{"UserInfo fallback", oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: `{"sub":"user-one","picture":"https://new.test/private-path.png?secret=private-query"}`}, "https://new.test/private-path.png?secret=private-query", "userinfo.picture", "new.test", true, false},
		{"preserved during outage", oidcFixture{claims: map[string]any{"name": "token-name"}, userInfoStatus: http.StatusServiceUnavailable}, "https://old.test/private-path.png?secret=private-query", "none", "", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, start := startOIDCFixture(t, tt.fixture)
			_, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: s.Config.OIDCissuer, Subject: "user-one", AvatarURL: "https://old.test/private-path.png?secret=private-query"})
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
			result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
			if err != nil || result.Credentials == nil {
				t.Fatal("verified login failed", err)
			}
			if result.Credentials.Session.AvatarURL != tt.wantAvatar {
				t.Fatalf("effective avatar=%q, want %q", result.Credentials.Session.AvatarURL, tt.wantAvatar)
			}
			var foundDecision, foundEffective bool
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if entry["avatar_source"] == tt.wantSource && entry["avatar_host"] == tt.decisionHost && entry["avatar_fallback"] == tt.wantFallback && entry["avatar_preserve_existing"] == tt.wantPreserved {
					foundDecision = true
				}
				if entry["user_id"] == result.Credentials.Session.UserID && entry["identity_id"] == result.Credentials.Session.IdentityID && entry["provider_id"] == "moeauth" && entry["avatar_present"] == true && entry["avatar_preserved"] == tt.wantPreserved {
					foundEffective = true
				}
			}
			if !foundDecision || !foundEffective {
				t.Fatalf("missing avatar decision/effective diagnostic: %s", logs.String())
			}
			for _, secret := range []string{"private-path", "private-query", result.Credentials.Token, "test-access-token"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("diagnostic exposed %q", secret)
				}
			}
		})
	}
}

func TestStoredAvatarDiagnosticsDoNotExposeURLSecrets(t *testing.T) {
	for _, tt := range []struct {
		name, raw, status, host string
	}{
		{"present", "https://cdn.test/private-path.png?secret=private-query#private-fragment", "present", "cdn.test"},
		{"missing", "", "missing", ""},
		{"legacy unsafe value", "https://private-user:private-password@cdn.test/private-path.png?secret=private-query", "rejected", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testAuth(t)
			s.Config.OIDCid = "moeauth"
			c, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: "https://issuer.test", Subject: "test-subject"})
			if err != nil {
				t.Fatal(err)
			}
			// Imported identities may predate current URL validation. Reads must
			// diagnose stored values without silently rewriting the profile.
			if _, err := s.Store.DB.Exec("UPDATE identities SET avatar_url=?", tt.raw); err != nil {
				t.Fatal(err)
			}
			session, err := s.Resolve(context.Background(), c.Token)
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			profile, err := s.Profile(context.Background(), session)
			if err != nil || profile.AvatarURL != tt.raw || len(profile.Bindings) != 1 || profile.Bindings[0].AvatarURL != tt.raw {
				t.Fatalf("stored avatar changed during profile read: %+v, %v", profile, err)
			}
			var entry map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
				t.Fatalf("missing profile avatar diagnostic: %q, %v", logs.String(), err)
			}
			if entry["level"] != "DEBUG" || entry["user_id"] != session.UserID || entry["identity_id"] != session.IdentityID || entry["provider_id"] != "moeauth" || entry["avatar_source"] != "stored_identity" || entry["avatar_present"] != (tt.raw != "") || entry["avatar_status"] != tt.status || entry["avatar_host"] != tt.host {
				t.Fatalf("incomplete stored avatar diagnostic: %v", entry)
			}
			for _, secret := range []string{"private-path", "private-query", "private-fragment", "private-user", "private-password", c.Token, "test-subject"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("avatar diagnostic exposed sensitive value %q", secret)
				}
			}
		})
	}
}
