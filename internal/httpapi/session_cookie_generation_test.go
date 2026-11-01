package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestDelayedSuccessfulCookieResponsesPreserveNewLogin(t *testing.T) {
	for _, action := range []string{"logout", "renew", "business"} {
		t.Run(action, func(t *testing.T) {
			s, h, a := setup(t)
			s.Config.Development, a.Config.Development = false, false
			s.Config.PublicURL, a.Config.PublicURL = "https://donate.example", "https://donate.example"
			a.Config.QQappID, a.Config.QQsecret = "123", "secret"
			ctx := context.Background()
			bob, err := a.LoginIdentity(ctx, auth.Identity{Provider: "qq", Issuer: "123", Subject: "bob-openid", QQNumber: "12345"})
			if err != nil {
				t.Fatal(err)
			}
			old, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			a.HTTP = &http.Client{Transport: reauthenticationTransport(func(r *http.Request) (*http.Response, error) {
				body := map[string]string{
					"/oauth2.0/token":     `{"access_token":"token"}`,
					"/oauth2.0/me":        `{"client_id":"123","openid":"bob-openid"}`,
					"/user/get_user_info": `{"ret":0,"nickname":"Bob","figureurl_qq_2":"https://qlogo.cn/bob"}`,
				}[r.URL.Path]
				if body == "" {
					t.Fatalf("unexpected upstream URL %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			origin, _ := url.Parse(s.Config.PublicURL)
			jar, _ := cookiejar.New(nil)
			initial := httptest.NewRecorder()
			s.setCookie(initial, s.sessionName(), old.Token, old.Session.ExpiresAt)
			jar.SetCookies(origin, initial.Result().Cookies())
			send := func(method, path, body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, path, strings.NewReader(body))
				if method == "POST" {
					r.Header.Set("Origin", s.Config.PublicURL)
					r.Header.Set("X-CSRF-Token", old.Session.CSRF)
				}
				for _, cookie := range jar.Cookies(origin) {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			begin := send("POST", "/api/v1/user/login", `{"provider":"qq","return_to":"/"}`)
			var start struct {
				Code string
				Data map[string]string
			}
			if err := json.Unmarshal(begin.Body.Bytes(), &start); err != nil || start.Code != "OK" {
				t.Fatalf("begin: %d %s (%v)", begin.Code, begin.Body, err)
			}
			authorize, err := url.Parse(start.Data["authorization_url"])
			if err != nil {
				t.Fatal(err)
			}
			jar.SetCookies(origin, begin.Result().Cookies())
			if action != "logout" {
				uid, _ := storage.ParseID(old.Session.UserID)
				if _, err := a.Store.DB.Exec("UPDATE sessions SET expires_at=? WHERE user_id=?", a.Now().Add(auth.RenewWindow).Unix(), uid); err != nil {
					t.Fatal(err)
				}
			}
			path, body := "/api/v1/user/session/renew", `{}`
			if action == "logout" {
				path = "/api/v1/user/logout"
			}
			if action == "business" {
				path, body = "/api/v1/order/create", `{"amount_fen":1,"channel":"alipay","idempotency_key":"old-business"}`
			}
			delayed := send("POST", path, body)
			if delayed.Code != 200 || !strings.Contains(delayed.Body.String(), `"code":"OK"`) {
				t.Fatalf("old response: %d %s", delayed.Code, delayed.Body)
			}
			login := send("GET", "/api/v1/user/oauth/callback/qq?state="+authorize.Query().Get("state")+"&code=test-code", "")
			if login.Code != 303 || login.Header().Get("Location") != "/" {
				t.Fatalf("callback: %d %s", login.Code, login.Header().Get("Location"))
			}
			jar.SetCookies(origin, login.Result().Cookies())
			before := send("GET", "/api/v1/user/profile", "")
			if before.Code != 200 || !strings.Contains(before.Body.String(), bob.Session.UserID) {
				t.Fatalf("new login: %d %s", before.Code, before.Body)
			}
			if _, err := a.Resolve(ctx, old.Token); err != auth.ErrUnauthorized {
				t.Fatalf("old token not revoked: %v", err)
			}
			// Apply real Secure/HttpOnly/__Host Set-Cookie on HTTPS in delivery order.
			jar.SetCookies(origin, delayed.Result().Cookies())
			after := send("GET", "/api/v1/user/profile", "")
			if after.Code != 200 || !strings.Contains(after.Body.String(), bob.Session.UserID) {
				t.Fatalf("late successful %s destroyed newer login: %d %s", action, after.Code, after.Body)
			}
		})
	}
}

func TestDelayedSuccessfulSelfUnbindPreservesNewLogin(t *testing.T) {
	s, h, _, member, _ := httpIdentityFixture(t)
	s.Config.Development, s.Auth.Config.Development = false, false
	s.Config.PublicURL, s.Auth.Config.PublicURL = "https://donate.example", "https://donate.example"
	s.Config.AdminIDs[member.Session.UserID] = true
	origin, _ := url.Parse(s.Config.PublicURL)
	jar, _ := cookiejar.New(nil)
	initial := httptest.NewRecorder()
	s.writeSessionCookie(initial, httptest.NewRequest("GET", "/", nil), member.Token, member.Session)
	jar.SetCookies(origin, initial.Result().Cookies())
	r := httptest.NewRequest("POST", "/api/v1/admin/user/identity/unbind", strings.NewReader(fmt.Sprintf(`{"id":%q,"identity_id":%q}`, member.Session.UserID, member.Session.IdentityID)))
	r.Header.Set("Origin", s.Config.PublicURL)
	r.Header.Set("X-CSRF-Token", member.Session.CSRF)
	for _, c := range jar.Cookies(origin) {
		r.AddCookie(c)
	}
	delayed := httptest.NewRecorder()
	h.ServeHTTP(delayed, r)
	if delayed.Code != 200 || !strings.Contains(delayed.Body.String(), `"code":"OK"`) {
		t.Fatalf("self-unbind failed: %d %s", delayed.Code, delayed.Body)
	}
	newest, err := s.Auth.LoginIdentity(auth.WithBrowserFamilies(context.Background(), []string{member.Session.BrowserFamily}), auth.Identity{Provider: "qq", Issuer: "test", Subject: "member-qq", QQNumber: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	issued := httptest.NewRecorder()
	s.writeSessionCookie(issued, httptest.NewRequest("GET", "/", nil), newest.Token, newest.Session)
	jar.SetCookies(origin, issued.Result().Cookies())
	jar.SetCookies(origin, delayed.Result().Cookies())
	profile := httptest.NewRequest("GET", "/api/v1/user/profile", nil)
	for _, c := range jar.Cookies(origin) {
		profile.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, profile)
	if w.Code != 200 || !strings.Contains(w.Body.String(), newest.Session.CSRF) {
		t.Fatalf("late successful self-unbind destroyed current session: %d %s", w.Code, w.Body)
	}
}
