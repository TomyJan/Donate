package httpapi

import (
	"bytes"
	"context"
	"errors"
	"github.com/TomyJan/Donate/internal/auth"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type reauthenticationTransport func(*http.Request) (*http.Response, error)

func (f reauthenticationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRegressionQQReauthenticationReturnsSuccess(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	a.Config.QQappID = "123"
	a.Config.QQsecret = "secret"
	old, e := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "i", Subject: "old"})
	if e != nil {
		t.Fatal(e)
	}
	var picture bytes.Buffer
	if e := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); e != nil {
		t.Fatal(e)
	}
	a.HTTP = &http.Client{Transport: reauthenticationTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/oauth2.0/token":
			body = "{\"access_token\":\"token\"}"
		case "/oauth2.0/me":
			body = "{\"client_id\":\"123\",\"openid\":\"new-qq\"}"
		case "/user/get_user_info":
			body = "{\"ret\":0,\"nickname\":\"New QQ\",\"figureurl_qq_2\":\"https://qlogo.cn/new\"}"
		default:
			body = picture.String()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	flow, e := a.BeginFlow(ctx, "qq", "login", &old.Session, "/")
	if e != nil {
		t.Fatal(e)
	}
	result, e := a.Callback(ctx, "qq", flow.State, "code", flow.Cookie, &old.Session)
	if e != nil || !result.NeedQQ {
		t.Fatalf("prepare callback: %+v %v", result, e)
	}
	req := httptest.NewRequest("POST", "/api/v1/user/login/qq/complete", strings.NewReader("{\"qq_number\":\"12345\"}"))
	req.Header.Set("Origin", s.Config.PublicURL)
	req.Header.Set("X-CSRF-Token", old.Session.CSRF)
	req.AddCookie(&http.Cookie{Name: s.sessionName(), Value: old.Token})
	req.AddCookie(&http.Cookie{Name: s.cookieName("flow"), Value: flow.Cookie})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	newValid := false
	var completed auth.Session
	for _, c := range w.Result().Cookies() {
		if strings.HasPrefix(c.Name, s.sessionName()+"_") && c.Value != "" {
			if session, e := a.Resolve(ctx, c.Value); e == nil {
				newValid = true
				completed = session
			}
		}
	}
	t.Logf("status=%d body=%s new_cookie_valid=%v", w.Code, w.Body.String(), newValid)
	if w.Code != 200 || !newValid {
		t.Fatal("successful reauthentication returned a failure")
	}
	if _, e = a.Resolve(ctx, old.Token); !errors.Is(e, auth.ErrUnauthorized) {
		t.Fatal("old credential remains usable", e)
	}
	newest, e := a.LoginIdentity(auth.WithBrowserFamilies(ctx, []string{completed.BrowserFamily}), auth.Identity{Provider: "qq", Issuer: "123", Subject: "new-qq", QQNumber: "12345"})
	if e != nil {
		t.Fatal(e)
	}
	origin, _ := url.Parse(s.Config.PublicURL)
	jar, _ := cookiejar.New(nil)
	issued := httptest.NewRecorder()
	s.writeSessionCookie(issued, httptest.NewRequest("GET", "/", nil), newest.Token, newest.Session)
	jar.SetCookies(origin, issued.Result().Cookies())
	jar.SetCookies(origin, w.Result().Cookies())
	profile := httptest.NewRequest("GET", "/api/v1/user/profile", nil)
	for _, c := range jar.Cookies(origin) {
		profile.AddCookie(c)
	}
	current := httptest.NewRecorder()
	h.ServeHTTP(current, profile)
	if current.Code != 200 || !strings.Contains(current.Body.String(), newest.Session.CSRF) {
		t.Fatalf("late successful QQ completion replaced current session: %d %s", current.Code, current.Body)
	}
}
