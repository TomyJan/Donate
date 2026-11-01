package httpapi

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
)

func TestSessionBoundReadsNeverReturnAnotherCookieSessionData(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	old, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "reader-a"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "reader-b"})
	if err != nil {
		t.Fatal(err)
	}
	s.Config.AdminIDs[current.Session.UserID] = true
	for _, path := range []string{"/api/v1/order/list?only_mine=true", "/api/v1/order/detail?id=missing", "/api/v1/admin/overview", "/api/v1/admin/user/list", "/api/v1/admin/task/list"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest("GET", path, nil)
			r.AddCookie(&http.Cookie{Name: s.sessionCookieName(current.Session.ID), Value: current.Token})
			r.Header.Set("X-Session-Scope", old.Session.CSRF)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 || !strings.Contains(w.Body.String(), `"code":"CSRF_FAILED"`) || w.Header().Get("X-Session-Mismatch") != "1" {
				t.Fatalf("cross-session read reached handler: %d %s", w.Code, w.Body)
			}
		})
	}
	for _, expected := range []string{"", current.Session.CSRF} {
		r := httptest.NewRequest("GET", "/api/v1/order/list?only_mine=true", nil)
		r.AddCookie(&http.Cookie{Name: s.sessionCookieName(current.Session.ID), Value: current.Token})
		r.Header.Set("X-Session-Scope", expected)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("current/legacy read rejected: %d %s", w.Code, w.Body)
		}
	}
	for _, path := range []string{"/api/v1/user/profile", "/api/v1/site/info"} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: s.sessionCookieName(current.Session.ID), Value: current.Token})
		r.Header.Set("X-Session-Scope", old.Session.CSRF)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("session discovery rejected: %d %s", w.Code, w.Body)
		}
	}
	for _, expected := range []string{old.Session.CSRF, "guest"} {
		r := httptest.NewRequest("GET", "/api/v1/order/list", nil)
		if expected == "guest" {
			r.AddCookie(&http.Cookie{Name: s.sessionCookieName(current.Session.ID), Value: current.Token})
		}
		r.Header.Set("X-Session-Scope", expected)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || w.Header().Get("X-Session-Mismatch") != "1" {
			t.Fatalf("guest/account switch accepted: %d %s", w.Code, w.Body)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/order/list", nil)
	r.Header.Set("X-Session-Scope", "guest")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("normal anonymous list rejected: %d %s", w.Code, w.Body)
	}
}

func TestGuestScopeWithInvalidLegacyCookieStillReadsPublicFeed(t *testing.T) {
	s, h, a := setup(t)
	s.Config.Development, a.Config.Development = false, false
	s.Config.PublicURL, a.Config.PublicURL = "https://donate.example", "https://donate.example"
	origin, _ := url.Parse(s.Config.PublicURL)
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(origin, []*http.Cookie{{Name: s.sessionName(), Value: strings.Repeat("x", 43), Path: "/", Secure: true, HttpOnly: true}})
	for i := 0; i < 3; i++ {
		for _, path := range []string{"/api/v1/user/profile", "/api/v1/order/list", "/api/v1/order/detail?id=missing"} {
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("X-Session-Scope", "guest")
			for _, c := range jar.Cookies(origin) {
				r.AddCookie(c)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			jar.SetCookies(origin, w.Result().Cookies())
			want := 401
			if path == "/api/v1/order/list" {
				want = 200
			}
			if w.Code != want || w.Header().Get("X-Session-Mismatch") != "" {
				t.Fatalf("round %d %s: %d %s", i, path, w.Code, w.Body)
			}
		}
	}
}
