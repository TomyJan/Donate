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

// These cases catch dropping the provider's advertised avatar claim, selecting an
// unusable picture before validating it, or accepting another subject's profile.
func TestOIDCAvatarCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture oidcFixture
		want    string
	}{
		{"Casdoor token avatar without UserInfo endpoint", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "https://cdn.test/avatar.png"}, noUserInfo: true}, "https://cdn.test/avatar.png"},
		{"Casdoor token avatar survives unavailable UserInfo", oidcFixture{claims: map[string]any{"avatar": "https://cdn.test/avatar.png"}, userInfoStatus: http.StatusServiceUnavailable, userInfo: "unavailable"}, "https://cdn.test/avatar.png"},
		{"UserInfo avatar alias", oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: `{"sub":"user-one","avatar":"https://cdn.test/info.png"}`}, "https://cdn.test/info.png"},
		{"standard token picture precedes alias", oidcFixture{claims: map[string]any{"name": "token-name", "picture": "https://cdn.test/picture.png", "avatar": "https://cdn.test/avatar.png"}, noUserInfo: true}, "https://cdn.test/picture.png"},
		{"standard UserInfo picture precedes token alias", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "https://cdn.test/avatar.png"}, userInfo: `{"sub":"user-one","picture":"https://cdn.test/info.png"}`}, "https://cdn.test/info.png"},
		{"unsafe token picture falls back to UserInfo", oidcFixture{claims: map[string]any{"name": "token-name", "picture": "http://cdn.test/token.png"}, userInfo: `{"sub":"user-one","picture":"https://cdn.test/info.png"}`}, "https://cdn.test/info.png"},
		{"unsafe picture falls back to safe token alias", oidcFixture{claims: map[string]any{"name": "token-name", "picture": "http://cdn.test/token.png", "avatar": "https://cdn.test/avatar.png"}, noUserInfo: true}, "https://cdn.test/avatar.png"},
		{"safe token alias survives malformed UserInfo", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "https://cdn.test/avatar.png"}, userInfo: `{"sub":"user-one","preferred_username":42,"picture":"https://cdn.test/info.png"}`}, "https://cdn.test/avatar.png"},
		{"mismatched subject cannot supply alias", oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: `{"sub":"someone-else","avatar":"https://cdn.test/info.png"}`}, ""},
		{"protocol-relative avatar remains rejected", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "//cdn.test/avatar.png"}, noUserInfo: true}, ""},
		{"relative avatar remains rejected", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "/avatar.png"}, noUserInfo: true}, ""},
		{"unavailable HTTPS upgrade remains rejected", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "http://cdn.test/avatar.png"}, noUserInfo: true}, ""},
		{"private IP alias remains rejected", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "https://127.0.0.1/avatar.png"}, noUserInfo: true}, ""},
		{"short IPv4 alias remains rejected", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "https://127.1/avatar.png"}, noUserInfo: true}, ""},
		{"IPv4-mapped private alias remains rejected", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "https://[::ffff:10.0.0.1]/avatar.png"}, noUserInfo: true}, ""},
		{"local hostname alias remains rejected", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "https://localhost/avatar.png"}, noUserInfo: true}, ""},
		{"non-string alias does not reject verified login", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": 42}, noUserInfo: true}, ""},
		{"non-string alias cannot poison safe picture", oidcFixture{claims: map[string]any{"name": "token-name", "picture": "https://cdn.test/picture.png", "avatar": 42}, noUserInfo: true}, "https://cdn.test/picture.png"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, start := startOIDCFixture(t, tt.fixture)
			previous, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: s.Config.OIDCissuer, Subject: "user-one", Nickname: "old-name"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
			if err != nil || result.Credentials == nil {
				t.Fatal("verified login failed", err)
			}
			if result.Credentials.Session.UserID != previous.Session.UserID || result.Credentials.Session.IdentityID != previous.Session.IdentityID {
				t.Fatal("avatar enrichment changed account or identity")
			}
			for _, token := range []string{previous.Token, result.Credentials.Token} {
				session, err := s.Resolve(context.Background(), token)
				if err != nil || session.AvatarURL != tt.want {
					t.Fatalf("resolved avatar=%q, want %q: %v", session.AvatarURL, tt.want, err)
				}
				profile, err := s.Profile(context.Background(), session)
				if err != nil || profile.AvatarURL != tt.want || len(profile.Bindings) != 1 || profile.Bindings[0].AvatarURL != tt.want {
					t.Fatalf("profile/binding avatar not refreshed: %+v %v", profile, err)
				}
			}
			var avatar string
			if err := s.Store.DB.QueryRow("SELECT avatar_url FROM identities").Scan(&avatar); err != nil || avatar != tt.want {
				t.Fatalf("persisted avatar=%q, want %q: %v", avatar, tt.want, err)
			}
		})
	}
}

// Exercise browser IDNA mappings through each verified profile source. Dropping
// normalization would let Unicode local/private hosts reach persisted avatars;
// rejecting all Unicode would break legitimate internationalized public names.
func TestOIDCAvatarUnicodeHosts(t *testing.T) {
	for _, tt := range []struct {
		name, raw, want string
	}{
		{"mixed fullwidth loopback", "https://127.0.0.１/avatar.png", ""},
		{"fullwidth loopback", "https://１２７.０.０.１/avatar.png", ""},
		{"escaped fullwidth loopback", "https://127.0.0.%EF%BC%91/avatar.png", ""},
		{"fullwidth private IP", "https://10.0.0.１/avatar.png", ""},
		{"escaped fullwidth private IP", "https://10.0.0.%EF%BC%91/avatar.png", ""},
		{"fullwidth localhost suffix", "https://foo.localｈost/avatar.png", ""},
		{"escaped fullwidth localhost suffix", "https://foo.local%EF%BD%88ost/avatar.png", ""},
		{"Unicode dot and localhost suffix", "https://foo.bar。localｈost/avatar.png", ""},
		{"Unicode dot and local suffix", "https://foo.bar．ｌocal/avatar.png", ""},
		{"zero width localhost suffix", "https://foo.local​host/avatar.png", ""},
		{"public Latin IDN", "https://bücher.example/avatar.png", "https://xn--bcher-kva.example/avatar.png"},
		{"public CJK IDN", "https://例子.测试/avatar.png", "https://xn--fsqu00a.xn--0zwm56d/avatar.png"},
		{"public IDN keeps port and query", "https://bücher。example:8443/avatar.png?size=100#v", "https://xn--bcher-kva.example:8443/avatar.png?size=100#v"},
		{"public IPv6 stays valid", "https://[2606:4700:4700::1111]/avatar.png", "https://[2606:4700:4700::1111]/avatar.png"},
		{"invalid IDNA joiner rejected", "https://foo.‍example/avatar.png", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, source := range []string{"id_token.picture", "id_token.avatar", "userinfo.picture", "userinfo.avatar"} {
				t.Run(source, func(t *testing.T) {
					parts := strings.Split(source, ".")
					fixture := oidcFixture{claims: map[string]any{"name": "token-name"}}
					if parts[0] == "id_token" {
						fixture.claims[parts[1]] = tt.raw
						fixture.noUserInfo = true
					} else {
						info, err := json.Marshal(map[string]any{"sub": "user-one", parts[1]: tt.raw})
						if err != nil {
							t.Fatal(err)
						}
						fixture.userInfo = string(info)
					}
					s, start := startOIDCFixture(t, fixture)
					result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
					if err != nil || result.Credentials == nil {
						t.Fatal("verified login failed", err)
					}
					session, err := s.Resolve(context.Background(), result.Credentials.Token)
					if err != nil || session.AvatarURL != tt.want {
						t.Fatalf("resolved avatar=%q, want %q: %v", session.AvatarURL, tt.want, err)
					}
					profile, err := s.Profile(context.Background(), session)
					if err != nil || profile.AvatarURL != tt.want || len(profile.Bindings) != 1 || profile.Bindings[0].AvatarURL != tt.want {
						t.Fatalf("unsafe or missing profile/binding avatar: %+v %v", profile, err)
					}
				})
			}
		})
	}
}

// Missing fields and rejected sources must be diagnosable without exposing the
// token, UserInfo body, URL credentials, or query parameters.
func TestOIDCAvatarDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name                               string
		fixture                            oidcFixture
		wantSource, wantStatus, wantReason string
	}{
		{"token alias", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": "https://cdn.test/avatar.png?secret=private-query"}, noUserInfo: true}, "id_token.avatar", "accepted", ""},
		{"missing endpoint", oidcFixture{claims: map[string]any{"name": "token-name"}, noUserInfo: true}, "none", "missing", "userinfo_unavailable"},
		{"mismatched subject", oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: `{"sub":"somebody-else","avatar":"https://private.test/avatar.png?secret=private-query"}`}, "none", "missing", "userinfo_subject_mismatch"},
		{"rejected credentials", oidcFixture{claims: map[string]any{"name": "token-name", "picture": "https://private-user:private-password@private.test/avatar.png?secret=private-query"}, userInfo: `{"sub":"user-one"}`}, "none", "missing", "avatar_rejected"},
		{"empty fields", oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: `{"sub":"user-one"}`}, "none", "missing", "avatar_absent"},
		{"upstream body stays private", oidcFixture{claims: map[string]any{"name": "token-name"}, userInfoStatus: http.StatusServiceUnavailable, userInfo: `private-password https://private.test/avatar?secret=private-query`}, "none", "missing", "userinfo_unavailable"},
		{"malformed enrichment stays private", oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: `{"sub":"user-one","preferred_username":42,"picture":"https://private.test/avatar?secret=private-query"}`}, "none", "missing", "userinfo_invalid_claims"},
		{"wrong alias type diagnosed", oidcFixture{claims: map[string]any{"name": "token-name", "avatar": 42}, noUserInfo: true}, "none", "missing", "avatar_rejected"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, start := startOIDCFixture(t, tt.fixture)
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
			result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
			if err != nil || result.Credentials == nil {
				t.Fatal("verified login failed", err)
			}
			found := false
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if entry["avatar_status"] == tt.wantStatus && entry["avatar_source"] == tt.wantSource && entry["avatar_reason"] == tt.wantReason {
					if entry["provider_id"] != "moeauth" || entry["subject"] != "user-one" {
						t.Fatalf("diagnostic cannot identify verified identity: %v", entry)
					}
					message, _ := entry["msg"].(string)
					if !strings.Contains(message, "moeauth") || !strings.Contains(message, "user-one") {
						t.Fatalf("diagnostic message lacks provider/subject: %q", message)
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("missing avatar diagnostic source=%s status=%s reason=%s: %s", tt.wantSource, tt.wantStatus, tt.wantReason, logs.String())
			}
			for _, secret := range []string{"test-access-token", "eyJ", "private-user", "private-password", "private.test", "private-query", "somebody-else"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("sensitive avatar/profile data appeared in diagnostic: %q", secret)
				}
			}
		})
	}
}
