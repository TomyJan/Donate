package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestBannedCookieAllowsAnonymousRecoveryButDeniesProtectedActions(t *testing.T) {
	testUnusableCookieRecovery(t, "banned")
}

func TestRevokedCookieAllowsAnonymousRecoveryButDeniesProtectedActions(t *testing.T) {
	testUnusableCookieRecovery(t, "revoked")
}

func testUnusableCookieRecovery(t *testing.T, invalidation string) {
	t.Helper()
	s, h, a := setup(t)
	a.Config.QQappID = "123"
	ctx := context.Background()
	identity := auth.Identity{Provider: "oidc", Issuer: "test", Subject: "recovery-" + invalidation}
	c, err := a.LoginIdentity(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus, wantCode := http.StatusForbidden, "ACCOUNT_BANNED"
	if invalidation == "banned" {
		uid, _ := storage.ParseID(c.Session.UserID)
		if _, err := a.Store.DB.Exec("UPDATE users SET status='banned' WHERE id=?", uid); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := a.Logout(ctx, c.Token); err != nil {
			t.Fatal(err)
		}
		wantStatus, wantCode = http.StatusUnauthorized, "UNAUTHORIZED"
	}
	request := func(method, path, body, csrf string, extra *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", s.Config.PublicURL)
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
		if extra != nil {
			r.AddCookie(extra)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == s.sessionName() {
				t.Errorf("request carrying an unusable token must not overwrite a newer session cookie for %s", path)
			}
		}
		return w
	}
	info := request("GET", "/api/v1/site/info", "", "", nil)
	var site struct {
		Code string
		Data struct {
			CSRF string `json:"csrf_token"`
		}
	}
	if err := json.Unmarshal(info.Body.Bytes(), &site); err != nil || info.Code != 200 || site.Code != "OK" || site.Data.CSRF == "" || site.Data.CSRF == c.Session.CSRF {
		t.Fatalf("anonymous site info: %d %s %v", info.Code, info.Body.String(), err)
	}
	var anonymous *http.Cookie
	for _, cookie := range info.Result().Cookies() {
		if cookie.Name == s.cookieName("csrf") {
			anonymous = cookie
		}
	}
	if anonymous == nil {
		t.Fatal("anonymous CSRF cookie missing")
	}
	list := request("GET", "/api/v1/order/list", "", "", nil)
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"code":"OK"`) {
		t.Fatalf("public list: %d %s", list.Code, list.Body.String())
	}
	login := request("POST", "/api/v1/user/login", `{"provider":"qq","return_to":"/"}`, site.Data.CSRF, anonymous)
	if login.Code != 200 || !strings.Contains(login.Body.String(), `"code":"OK"`) {
		t.Fatalf("login recovery: %d %s", login.Code, login.Body.String())
	}
	for _, path := range []string{"/api/v1/order/create", "/api/v1/user/identity/bind", "/api/v1/user/logout", "/api/v1/admin/order/action/execute"} {
		w := request("POST", path, `{}`, c.Session.CSRF, nil)
		if w.Code != wantStatus || !strings.Contains(w.Body.String(), wantCode) {
			t.Errorf("protected action %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	profile := request("GET", "/api/v1/user/profile", "", "", nil)
	if profile.Code != wantStatus || !strings.Contains(profile.Body.String(), wantCode) {
		t.Errorf("profile: %d %s", profile.Code, profile.Body.String())
	}
	_, err = a.LoginIdentity(ctx, identity)
	if invalidation == "banned" && !errors.Is(err, auth.ErrBanned) {
		t.Fatalf("banned identity can log in: %v", err)
	} else if invalidation == "revoked" && err != nil {
		t.Fatalf("revoked session prevented fresh identity login: %v", err)
	}
}
