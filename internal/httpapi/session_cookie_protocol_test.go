package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestLegacyCookieMigratesToIndependentSecureSlot(t *testing.T) {
	s, h, a := setup(t)
	s.Config.Development, a.Config.Development = false, false
	s.Config.PublicURL, a.Config.PublicURL = "https://donate.example", "https://donate.example"
	c, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "legacy-cookie"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/user/profile", nil)
	r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	foundSession, foundAnchor, removedLegacy := false, false, false
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == s.sessionName() && cookie.MaxAge < 0 {
			removedLegacy = true
			continue
		}
		if cookie.Name == s.sessionCookieName(c.Session.ID) {
			foundSession = true
			if cookie.Value != c.Token || !cookie.Expires.Equal(c.Session.ExpiresAt) {
				t.Fatal("migration changed credentials or expiry")
			}
		}
		if cookie.Name == s.browserCookieName(c.Session.BrowserFamily) {
			foundAnchor = true
			if !a.CheckBrowserProof(c.Session.BrowserFamily, cookie.Value) {
				t.Fatal("invalid anchor proof")
			}
		}
		if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("unsafe cookie: %+v", cookie)
		}
	}
	if !foundSession || !foundAnchor || !removedLegacy {
		t.Fatalf("incomplete legacy migration: %v %v %v", foundSession, foundAnchor, removedLegacy)
	}
	origin, _ := url.Parse(s.Config.PublicURL)
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(origin, w.Result().Cookies())
	request := httptest.NewRequest("GET", "/api/v1/user/profile", nil)
	for _, cookie := range jar.Cookies(origin) {
		request.AddCookie(cookie)
	}
	profile := httptest.NewRecorder()
	h.ServeHTTP(profile, request)
	if profile.Code != 200 || !strings.Contains(profile.Body.String(), c.Session.UserID) {
		t.Fatal("migrated jar cannot authenticate")
	}
}

func TestSessionCookieSuffixAndAnchorCannotForgeAuthentication(t *testing.T) {
	for _, kind := range []string{"wrong-suffix", "forged-anchor", "anchor-only", "duplicate-order"} {
		t.Run(kind, func(t *testing.T) {
			s, h, a := setup(t)
			c, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: kind})
			if err != nil {
				t.Fatal(err)
			}
			for _, reverse := range []bool{false, true} {
				r := httptest.NewRequest("GET", "/api/v1/user/profile", nil)
				cookies := []*http.Cookie{{Name: s.browserCookieName(c.Session.BrowserFamily), Value: a.BrowserProof(c.Session.BrowserFamily)}}
				status := 401
				switch kind {
				case "wrong-suffix":
					cookies = append(cookies, &http.Cookie{Name: s.sessionCookieName(storage.NewID().String()), Value: c.Token})
				case "forged-anchor":
					cookies = []*http.Cookie{{Name: s.browserCookieName(storage.NewID().String()), Value: a.BrowserProof(c.Session.BrowserFamily)}}
				case "duplicate-order":
					cookies = append(cookies, &http.Cookie{Name: s.sessionCookieName(c.Session.ID), Value: c.Token}, &http.Cookie{Name: s.sessionCookieName(c.Session.ID), Value: auth.RandomToken()})
					status = 200
				}
				for i := range cookies {
					j := i
					if reverse {
						j = len(cookies) - 1 - i
					}
					r.AddCookie(cookies[j])
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != status {
					t.Fatalf("reverse=%v: %d %s", reverse, w.Code, w.Body)
				}
			}
		})
	}
}

func TestOversizedBrowserCookiesFailClosedAndRecoverOnRetry(t *testing.T) {
	for _, kind := range []string{"sessions", "anchors", "invalid-anchors"} {
		t.Run(kind, func(t *testing.T) {
			s, h, a := setup(t)
			s.Config.Development, a.Config.Development = false, false
			s.Config.PublicURL, a.Config.PublicURL = "https://donate.example", "https://donate.example"
			origin, _ := url.Parse(s.Config.PublicURL)
			jar, _ := cookiejar.New(nil)
			for i := range auth.MaxBrowserCookies + 1 {
				id := storage.NewID().String()
				name, value := s.sessionCookieName(id), auth.RandomToken()
				if kind == "anchors" {
					credentials, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: fmt.Sprint(i)})
					if err != nil {
						t.Fatal(err)
					}
					id = credentials.Session.BrowserFamily
					name, value = s.browserCookieName(id), a.BrowserProof(id)
				} else if kind == "invalid-anchors" {
					name = s.browserCookieName(id)
				}
				jar.SetCookies(origin, []*http.Cookie{{Name: name, Value: value, Path: "/", Secure: true}})
			}
			current, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "current"})
			if err != nil {
				t.Fatal(err)
			}
			jar.SetCookies(origin, []*http.Cookie{{Name: s.sessionCookieName(current.Session.ID), Value: current.Token, Path: "/", Secure: true}})
			send := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/api/v1/user/profile", nil)
				for _, cookie := range jar.Cookies(origin) {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			blocked := send()
			if blocked.Code != 431 || strings.Contains(blocked.Body.String(), current.Session.UserID) {
				t.Fatalf("oversized request authenticated: %d %s", blocked.Code, blocked.Body)
			}
			jar.SetCookies(origin, blocked.Result().Cookies())
			retry := send()
			if retry.Code != 200 || !strings.Contains(retry.Body.String(), current.Session.UserID) {
				t.Fatalf("cleanup did not recover: %d %s", retry.Code, retry.Body)
			}
		})
	}
}

func TestRepeatedLoginsAndDelayedSlotsKeepCookieCountBounded(t *testing.T) {
	s, h, a := setup(t)
	s.Config.Development, a.Config.Development = false, false
	s.Config.PublicURL, a.Config.PublicURL = "https://donate.example", "https://donate.example"
	origin, _ := url.Parse(s.Config.PublicURL)
	jar, _ := cookiejar.New(nil)
	ctx := context.Background()
	var current, first auth.Credentials
	for i := range 24 {
		if i > 0 {
			ctx = auth.WithBrowserFamilies(context.Background(), []string{current.Session.BrowserFamily})
		}
		var err error
		current, err = a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "same-browser"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = current
		}
		r := httptest.NewRequest("GET", "/callback", nil)
		for _, cookie := range jar.Cookies(origin) {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.writeSessionCookie(w, r, current.Token, current.Session)
		jar.SetCookies(origin, w.Result().Cookies())
		if count := len(jar.Cookies(origin)); count != 2 {
			t.Fatalf("login %d retained %d cookies", i, count)
		}
	}
	jar.SetCookies(origin, []*http.Cookie{{Name: s.sessionCookieName(first.Session.ID), Value: first.Token, Path: "/", Secure: true}})
	r := httptest.NewRequest("GET", "/api/v1/user/profile", nil)
	for _, cookie := range jar.Cookies(origin) {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), current.Session.CSRF) {
		t.Fatalf("delayed slot changed current: %d %s", w.Code, w.Body)
	}
	jar.SetCookies(origin, w.Result().Cookies())
	if len(jar.Cookies(origin)) != 2 {
		t.Fatal("delayed obsolete slot not reclaimed")
	}
	var families int
	if err := a.Store.DB.QueryRow("SELECT count(*) FROM browser_families").Scan(&families); err != nil || families != 1 {
		t.Fatalf("repeated login grew families: %d %v", families, err)
	}
}

func TestAnonymousSiteReadsDoNotCreateBrowserMetadata(t *testing.T) {
	_, h, a := setup(t)
	for range 20 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/site/info", nil))
	}
	var count int
	if err := a.Store.DB.QueryRow("SELECT count(*) FROM browser_families").Scan(&count); err != nil || count != 0 {
		t.Fatalf("anonymous GET created family: %d %v", count, err)
	}
}
