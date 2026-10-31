package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

func configuredAuth(t *testing.T, values map[string]string) *Service {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, line := range strings.Split(string(config.Example()), "\n") {
		key, _, ok := strings.Cut(line, "=")
		if ok && !strings.HasPrefix(strings.TrimSpace(key), "#") {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(".env", config.Example(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
	for key, value := range values {
		t.Setenv(key, value)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := testAuth(t)
	s.Config = cfg
	return s
}

func TestOIDCCustomIDControlsAuthorizationAndCallback(t *testing.T) {
	s := configuredAuth(t, map[string]string{"OIDC_ISSUER": "https://issuer.test", "OIDC_CLIENT_ID": "client", "OIDC_ID": "campus", "OIDC_NAME": "校园账号", "OIDC_ICON_URL": "https://cdn.test/campus.svg"})
	s.HTTP.Transport = qqTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(
			`{"issuer":"https://issuer.test","authorization_endpoint":"https://issuer.test/authorize","token_endpoint":"https://issuer.test/token","jwks_uri":"https://issuer.test/keys","id_token_signing_alg_values_supported":["RS256"]}`))}, nil
	})
	f, err := s.BeginFlow(context.Background(), "campus", "login", nil, "/")
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(f.URL)
	if err != nil {
		t.Fatal(err)
	}
	if target.Query().Get("redirect_uri") != "http://127.0.0.1:8080/api/v1/user/oauth/callback/campus" {
		t.Fatal("custom callback ID not used", target.Query().Get("redirect_uri"))
	}
	status, err := s.FlowStatus(context.Background(), f.Cookie)
	if err != nil || status.Provider != "campus" {
		t.Fatal(status, err)
	}
	if _, err = s.BeginFlow(context.Background(), "oidc", "login", nil, "/"); !errors.Is(err, ErrInvalid) {
		t.Fatal("unconfigured alias accepted", err)
	}
}

func TestQQUnionIDPersistsBothIdentifiersAndUpgradesExistingAccount(t *testing.T) {
	s := configuredAuth(t, map[string]string{"QQ_APP_ID": "app", "QQ_APP_SECRET": "secret", "QQ_USE_UNIONID": "false"})
	ctx := context.Background()
	old, err := s.LoginIdentity(ctx, Identity{Provider: "qq", Issuer: "app", Subject: "open-one", QQNumber: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("QQ_USE_UNIONID", "true")
	s.Config, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.HTTP.Transport = qqTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"token"}`
		switch r.URL.Path {
		case "/oauth2.0/me":
			if r.URL.Query().Get("unionid") != "1" {
				t.Error("UnionID was not requested")
			}
			body = `{"client_id":"app","openid":"open-one","unionid":"union-one"}`
		case "/user/get_user_info":
			if r.URL.Query().Get("openid") != "open-one" {
				t.Error("user info requires OpenID")
			}
			body = `{"ret":0,"nickname":"QQ User"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	f, err := s.BeginFlow(ctx, "qq", "login", nil, "/")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Callback(ctx, "qq", f.State, "code", f.Cookie, nil)
	if err != nil || result.NeedQQ || result.Credentials == nil {
		t.Fatal("known QQ must not require number again", result, err)
	}
	if result.Credentials.Session.UserID != old.Session.UserID || result.Credentials.Session.IdentityID != old.Session.IdentityID {
		t.Fatal("enabling UnionID created a different account")
	}
	var subject, openid, unionid string
	if err = s.Store.DB.QueryRow("SELECT subject,qq_openid,qq_unionid FROM identities WHERE provider='qq'").Scan(&subject, &openid, &unionid); err != nil {
		t.Fatal(err)
	}
	if subject != "union-one" || openid != "open-one" || unionid != "union-one" {
		t.Fatal(subject, openid, unionid)
	}
	if _, err = s.Resolve(ctx, old.Token); err != nil {
		t.Fatal("existing session lost", err)
	}
	profile, err := s.Profile(ctx, result.Credentials.Session)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(profile)
	if strings.Contains(string(wire), "open-one") || strings.Contains(string(wire), "union-one") {
		t.Fatal("private identifiers leaked", string(wire))
	}
}

func TestQQUnionIDMissingIsNotSilentlyDowngraded(t *testing.T) {
	s := configuredAuth(t, map[string]string{"QQ_APP_ID": "app", "QQ_APP_SECRET": "secret", "QQ_USE_UNIONID": "true"})
	s.HTTP.Transport = qqTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"token"}`
		if r.URL.Path == "/oauth2.0/me" {
			body = `{"client_id":"app","openid":"open-one"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if _, err := s.exchangeQQ(context.Background(), "code"); err == nil {
		t.Fatal("missing UnionID accepted in UnionID mode")
	}
}

func TestQQIdentifierConflictCannotMoveOrMergeBindings(t *testing.T) {
	s := testAuth(t)
	ctx := context.Background()
	first, err := s.LoginIdentity(ctx, Identity{Provider: "qq", Issuer: "app", Subject: "open-a", QQNumber: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.LoginIdentity(ctx, Identity{Provider: "qq", Issuer: "app", Subject: "open-b", QQNumber: "234567"})
	if err != nil {
		t.Fatal(err)
	}
	s.Config.QQuseUnionID = true
	upgrade := Identity{Provider: "qq", Issuer: "app", Subject: "union-a", QQOpenID: "open-a", QQUnionID: "union-a", QQNumber: "123456"}
	if _, err = s.LoginIdentity(ctx, upgrade); err != nil {
		t.Fatal(err)
	}
	conflict := upgrade
	conflict.QQOpenID = "open-b"
	if _, err = s.LoginIdentity(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatal("different accounts merged", err)
	}
	if err = s.BindIdentity(ctx, second.Session.ID, upgrade); !errors.Is(err, ErrConflict) {
		t.Fatal("existing binding was moved", err)
	}
	upgrade.QQUnionID = "changed-union"
	upgrade.Subject = "changed-union"
	if _, err = s.LoginIdentity(ctx, upgrade); !errors.Is(err, ErrConflict) {
		t.Fatal("verified key silently replaced", err)
	}
	var count int
	if err = s.Store.DB.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 2 {
		t.Fatal("conflict mutated users", count, err)
	}
	s.Config.QQuseUnionID = false
	returned, err := s.LoginIdentity(ctx, Identity{Provider: "qq", Issuer: "app", Subject: "open-a", QQNumber: "123456"})
	if err != nil || returned.Session.UserID != first.Session.UserID {
		t.Fatal("disabling UnionID lost account", returned, err)
	}
	var union string
	if err = s.Store.DB.QueryRow("SELECT qq_unionid FROM identities WHERE qq_openid='open-a'").Scan(&union); err != nil || union != "union-a" {
		t.Fatal("saved UnionID lost", union, err)
	}
}
