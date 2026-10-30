package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

type oidcFixture struct {
	claims           map[string]any
	userInfo         string
	userInfoStatus   int
	noUserInfo       bool
	invalidSignature bool
}

// Exercise discovery, token exchange, JWKS verification, and UserInfo over HTTP.
func startOIDCFixture(t *testing.T, fixture oidcFixture) (*Service, FlowStart) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "oidc-test"))
	if err != nil {
		t.Fatal(err)
	}
	var issuer, nonce string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			discovery := map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}}
			if !fixture.noUserInfo {
				discovery["userinfo_endpoint"] = issuer + "/userinfo"
			}
			_ = json.NewEncoder(w).Encode(discovery)
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "oidc-test", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("code") != "code" || r.Form.Get("code_verifier") == "" {
				t.Error("invalid authorization code exchange")
				http.Error(w, "invalid exchange", http.StatusBadRequest)
				return
			}
			claims := map[string]any{"iss": issuer, "aud": "oidc-client", "sub": "user-one", "nonce": nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
			for field, value := range fixture.claims {
				claims[field] = value
			}
			payload, err := json.Marshal(claims)
			if err != nil {
				t.Error(err)
				return
			}
			signed, err := signer.Sign(payload)
			if err != nil {
				t.Error(err)
				return
			}
			raw, err := signed.CompactSerialize()
			if err != nil {
				t.Error(err)
				return
			}
			if fixture.invalidSignature {
				i := strings.LastIndex(raw, ".") + 1
				replacement := "A"
				if raw[i] == 'A' {
					replacement = "B"
				}
				raw = raw[:i] + replacement + raw[i+1:]
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access-token", "token_type": "Bearer", "expires_in": 60, "id_token": raw})
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer test-access-token" {
				t.Error("UserInfo requires the exchanged access token")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if fixture.userInfoStatus != 0 {
				w.WriteHeader(fixture.userInfoStatus)
			}
			_, _ = w.Write([]byte(fixture.userInfo))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	issuer = server.URL
	s := testAuth(t)
	s.Config.OIDCissuer, s.Config.OIDCclientID, s.Config.OIDCsecret = issuer, "oidc-client", "oidc-secret"
	s.Config.OIDCid, s.Config.PublicURL = "moeauth", "https://donate.test"
	start, err := s.BeginFlow(context.Background(), "moeauth", "login", nil, "/")
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := url.Parse(start.URL)
	if err != nil {
		t.Fatal(err)
	}
	nonce = authorization.Query().Get("nonce")
	return s, start
}

func TestOIDCUserInfoOnlyFillsMissingProfileClaims(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		fixture              oidcFixture
		wantName, wantAvatar string
	}{
		{
			name:     "preferred username remains authoritative",
			fixture:  oidcFixture{claims: map[string]any{"preferred_username": "token-user"}, userInfo: "{\"sub\":\"user-one\",\"name\":\"info-name\",\"picture\":\"https://cdn.test/info.png\"}"},
			wantName: "token-user", wantAvatar: "https://cdn.test/info.png",
		},
		{
			name:     "existing picture survives name enrichment",
			fixture:  oidcFixture{claims: map[string]any{"picture": "https://cdn.test/token.png"}, userInfo: "{\"sub\":\"user-one\",\"name\":\"info-name\",\"picture\":\"https://cdn.test/info.png\"}"},
			wantName: "info-name", wantAvatar: "https://cdn.test/token.png",
		},
		{
			name:     "all profile claims supplied by matching subject",
			fixture:  oidcFixture{userInfo: "{\"sub\":\"user-one\",\"preferred_username\":\"info-user\",\"picture\":\"https://cdn.test/info.png\"}"},
			wantName: "info-user", wantAvatar: "https://cdn.test/info.png",
		},
		{
			name:     "mismatched subject ignored",
			fixture:  oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: "{\"sub\":\"someone-else\",\"name\":\"info-name\",\"picture\":\"https://cdn.test/info.png\"}"},
			wantName: "token-name",
		},
		{
			name:     "missing subject ignored",
			fixture:  oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: "{\"name\":\"info-name\",\"picture\":\"https://cdn.test/info.png\"}"},
			wantName: "token-name",
		},
		{
			name:     "decode error does not partially overwrite claims",
			fixture:  oidcFixture{claims: map[string]any{"picture": "https://cdn.test/token.png"}, userInfo: "{\"sub\":\"user-one\",\"name\":\"partial-name\",\"preferred_username\":42,\"picture\":\"https://cdn.test/info.png\"}"},
			wantName: "捐赠者", wantAvatar: "https://cdn.test/token.png",
		},
		{
			name:     "upstream error preserves verified claims",
			fixture:  oidcFixture{claims: map[string]any{"picture": "https://cdn.test/token.png"}, userInfoStatus: http.StatusServiceUnavailable, userInfo: "unavailable"},
			wantName: "捐赠者", wantAvatar: "https://cdn.test/token.png",
		},
		{
			name:     "missing endpoint preserves verified claims",
			fixture:  oidcFixture{claims: map[string]any{"name": "token-name"}, noUserInfo: true},
			wantName: "token-name",
		},
		{
			name:     "unsafe avatar rejected",
			fixture:  oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: "{\"sub\":\"user-one\",\"picture\":\"http://cdn.test/info.png\"}"},
			wantName: "token-name",
		},
		{
			name:     "missing UserInfo picture leaves avatar empty",
			fixture:  oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: "{\"sub\":\"user-one\",\"name\":\"info-name\"}"},
			wantName: "token-name",
		},
		{
			name:     "malformed UserInfo preserves verified claims",
			fixture:  oidcFixture{claims: map[string]any{"name": "token-name"}, userInfo: "{invalid-json"},
			wantName: "token-name",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, start := startOIDCFixture(t, tt.fixture)
			result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
			if err != nil || result.Credentials == nil {
				t.Fatal("verified login failed", err)
			}
			session := result.Credentials.Session
			if session.Nickname != tt.wantName || session.AvatarURL != tt.wantAvatar {
				t.Fatalf("got nickname=%q avatar=%q, want nickname=%q avatar=%q", session.Nickname, session.AvatarURL, tt.wantName, tt.wantAvatar)
			}
			var issuer, subject string
			if err := s.Store.DB.QueryRow("SELECT issuer,subject FROM identities").Scan(&issuer, &subject); err != nil {
				t.Fatal(err)
			}
			if issuer != s.Config.OIDCissuer || subject != "user-one" {
				t.Fatal("UserInfo changed identity", issuer, subject)
			}
		})
	}
}

func TestOIDCUserInfoCannotBypassIDTokenVerification(t *testing.T) {
	for _, tt := range []struct {
		name             string
		claims           map[string]any
		invalidSignature bool
	}{
		{name: "issuer", claims: map[string]any{"iss": "https://other-issuer.test"}},
		{name: "audience", claims: map[string]any{"aud": "other-client"}},
		{name: "nonce", claims: map[string]any{"nonce": "wrong-nonce"}},
		{name: "expiry", claims: map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}},
		{name: "signature", invalidSignature: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, start := startOIDCFixture(t, oidcFixture{claims: tt.claims, invalidSignature: tt.invalidSignature, userInfo: "{\"sub\":\"user-one\",\"name\":\"info-name\",\"picture\":\"https://cdn.test/info.png\"}"})
			result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
			if !errors.Is(err, ErrUnauthorized) || result.Credentials != nil {
				t.Fatal("invalid ID token accepted", err)
			}
			var count int
			if err := s.Store.DB.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
				t.Fatal("invalid token created account", count, err)
			}
		})
	}
}

func TestOIDCNamedIDTokenLoadsMissingAvatarFromUserInfo(t *testing.T) {
	// Casdoor non-standard JWT uses name/avatar; UserInfo uses name/picture.
	s, start := startOIDCFixture(t, oidcFixture{
		claims:   map[string]any{"name": "token-name", "avatar": "https://cdn.test/avatar.png"},
		userInfo: `{"sub":"user-one","name":"userinfo-name","picture":"https://cdn.test/avatar.png"}`,
	})
	previous, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: s.Config.OIDCissuer, Subject: "user-one", Nickname: "old-name"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Callback(context.Background(), "moeauth", start.State, "code", start.Cookie, nil)
	if err != nil || result.Credentials == nil {
		t.Fatal("OIDC login failed", err)
	}
	if result.Credentials.Session.UserID != previous.Session.UserID || result.Credentials.Session.IdentityID != previous.Session.IdentityID {
		t.Fatal("profile enrichment created a different account or identity")
	}
	session, err := s.Resolve(context.Background(), result.Credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	previousSession, err := s.Resolve(context.Background(), previous.Token)
	if err != nil || previousSession.AvatarURL != "https://cdn.test/avatar.png" {
		t.Fatal("existing session did not receive refreshed avatar", err)
	}
	if session.AvatarURL != "https://cdn.test/avatar.png" || session.Nickname != "token-name" || session.Provider != "oidc" {
		t.Fatalf("unexpected persisted profile: avatar=%q nickname=%q provider=%q", session.AvatarURL, session.Nickname, session.Provider)
	}
	profile, err := s.Profile(context.Background(), session)
	if err != nil || profile.AvatarURL != "https://cdn.test/avatar.png" || len(profile.Bindings) != 1 || profile.Bindings[0].AvatarURL != "https://cdn.test/avatar.png" {
		t.Fatalf("avatar did not reach public profile and bindings: %+v %v", profile, err)
	}
}
