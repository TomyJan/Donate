package auth

import (
	"context"
	"net/http"
	"testing"
)

func TestOIDCLoginPreservesUnavailableProfileFields(t *testing.T) {
	const oldName, oldAvatar = "existing-name", "https://cdn.test/existing.png"
	for _, tc := range []struct {
		name                 string
		fixture              oidcFixture
		wantName, wantAvatar string
	}{
		{"outage", oidcFixture{userInfoStatus: http.StatusServiceUnavailable}, oldName, oldAvatar},
		{"token name during outage", oidcFixture{claims: map[string]any{"name": "new-name"}, userInfoStatus: http.StatusServiceUnavailable}, "new-name", oldAvatar},
		{"token picture during outage", oidcFixture{claims: map[string]any{"picture": "https://cdn.test/new.png"}, userInfoStatus: http.StatusServiceUnavailable}, oldName, "https://cdn.test/new.png"},
		{"token avatar during outage", oidcFixture{claims: map[string]any{"avatar": "https://cdn.test/new.png"}, userInfoStatus: http.StatusServiceUnavailable}, oldName, "https://cdn.test/new.png"},
		{"missing endpoint", oidcFixture{noUserInfo: true}, oldName, oldAvatar},
		{"subject mismatch", oidcFixture{userInfo: `{"sub":"different-user","name":"wrong-name","picture":"https://cdn.test/wrong.png"}`}, oldName, oldAvatar},
		{"invalid claims", oidcFixture{userInfo: `{"sub":"user-one","name":"partial-name","picture":42}`}, oldName, oldAvatar},
		{"explicit empty profile", oidcFixture{userInfo: `{"sub":"user-one","name":"","picture":""}`}, "捐赠者", ""},
		{"successful enrichment", oidcFixture{userInfo: `{"sub":"user-one","name":"new-name","picture":"https://cdn.test/new.png"}`}, "new-name", "https://cdn.test/new.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, start := startOIDCFixture(t, tc.fixture)
			previous, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: s.Config.OIDCissuer, Subject: "user-one", Nickname: oldName, AvatarURL: oldAvatar})
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
			if err != nil || result.Credentials == nil {
				t.Fatal("verified login failed", err)
			}
			if result.Credentials.Session.UserID != previous.Session.UserID || result.Credentials.Session.IdentityID != previous.Session.IdentityID {
				t.Fatal("callback created a different account or identity")
			}
			for _, token := range []string{previous.Token, result.Credentials.Token} {
				session, err := s.Resolve(context.Background(), token)
				if err != nil || session.Nickname != tc.wantName || session.AvatarURL != tc.wantAvatar {
					t.Fatalf("persisted profile: nickname=%q avatar=%q, want %q %q; error=%v", session.Nickname, session.AvatarURL, tc.wantName, tc.wantAvatar, err)
				}
				profile, err := s.Profile(context.Background(), session)
				if err != nil || len(profile.Bindings) != 1 || profile.Bindings[0].Nickname != tc.wantName || profile.Bindings[0].AvatarURL != tc.wantAvatar {
					t.Fatalf("binding profile mismatch: %+v %v", profile, err)
				}
			}
		})
	}
}
