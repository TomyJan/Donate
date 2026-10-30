package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
)

func TestSessionRenewSetsCookieOnlyWhenExpiryChanges(t *testing.T) {
	s, h, a := setup(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	a.Now = func() time.Time { return now }
	c, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "cookie-window"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		advance time.Duration
		cookies int
	}{
		{10*24*time.Hour - time.Second, 0},
		{time.Second, 1},
		{time.Second, 0},
	} {
		now = now.Add(tc.advance)
		r := httptest.NewRequest("POST", "/api/v1/user/session/renew", strings.NewReader("{}"))
		r.Header.Set("Origin", s.Config.PublicURL)
		r.Header.Set("X-CSRF-Token", c.Session.CSRF)
		r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var count int
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == s.sessionName() {
				count++
				if cookie.Value != c.Token {
					t.Fatal("credential rotated")
				}
				if !cookie.Expires.Equal(now.Add(30 * 24 * time.Hour)) {
					t.Fatalf("cookie expiry=%s", cookie.Expires)
				}
			}
		}
		if count != tc.cookies {
			t.Fatalf("session cookies=%d, want %d", count, tc.cookies)
		}
		if !strings.Contains(w.Body.String(), `"data":null`) {
			t.Fatalf("renew response changed: %s", w.Body.String())
		}
	}
}

func TestConcurrentSuccessfulPostsSetOneRenewalCookie(t *testing.T) {
	s, _, a := setup(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	a.Now = func() time.Time { return now }
	c, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "concurrent-cookies"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * 24 * time.Hour)
	var cookies atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Go(func() {
			<-start
			r := httptest.NewRequest("POST", "/api/v1/business", nil)
			r = r.WithContext(context.WithValue(r.Context(), stateKey{}, &requestState{Session: &c.Session, Token: c.Token}))
			w := httptest.NewRecorder()
			s.ok(w, r, map[string]string{"business": "preserved"})
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":{"business":"preserved"}`) {
				t.Errorf("business response changed: %d %s", w.Code, w.Body.String())
			}
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == s.sessionName() {
					cookies.Add(1)
				}
			}
		})
	}
	close(start)
	wg.Wait()
	if cookies.Load() != 1 {
		t.Fatalf("renewal cookies=%d, want 1", cookies.Load())
	}
}
