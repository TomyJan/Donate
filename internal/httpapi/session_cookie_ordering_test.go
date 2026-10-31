package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestLateUnusableSessionResponsesPreserveNewLogin(t *testing.T) {
	for _, invalidation := range []string{"revoked", "banned"} {
		t.Run(invalidation, func(t *testing.T) {
			s, h, a := setup(t)
			ctx := context.Background()
			old, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "old-response-" + invalidation})
			if err != nil {
				t.Fatal(err)
			}
			protectedStatus := http.StatusUnauthorized
			if invalidation == "revoked" {
				if err := a.Logout(ctx, old.Token); err != nil {
					t.Fatal(err)
				}
			} else {
				uid, _ := storage.ParseID(old.Session.UserID)
				if _, err := a.Store.DB.Exec("UPDATE users SET status='banned' WHERE id=?", uid); err != nil {
					t.Fatal(err)
				}
				protectedStatus = http.StatusForbidden
			}
			type delayedResponse struct {
				method, path string
				cookies      []*http.Cookie
			}
			delayed := []delayedResponse{}
			for _, route := range []struct {
				method, path string
				public       bool
			}{
				{"GET", "/api/v1/site/info", true},
				{"GET", "/api/v1/order/list", true},
				{"GET", "/api/v1/user/profile", false},
				{"POST", "/api/v1/order/create", false},
				{"POST", "/api/v1/user/identity/bind", false},
				{"POST", "/api/v1/user/logout", false},
				{"POST", "/api/v1/admin/order/action/execute", false},
			} {
				r := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
				r.Header.Set("Origin", s.Config.PublicURL)
				r.Header.Set("X-CSRF-Token", old.Session.CSRF)
				r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: old.Token})
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				want := protectedStatus
				if route.public {
					want = http.StatusOK
				}
				if w.Code != want {
					t.Fatalf("%s %s: HTTP %d, want %d: %s", route.method, route.path, w.Code, want, w.Body.String())
				}
				delayed = append(delayed, delayedResponse{route.method, route.path, w.Result().Cookies()})
			}
			// A later login has issued Bob's cookie before these buffered responses
			// reach the browser. Set-Cookie is applied even if JS ignores old data.
			bob, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "new-response-" + invalidation})
			if err != nil {
				t.Fatal(err)
			}
			login := httptest.NewRecorder()
			s.setCookie(login, s.sessionName(), bob.Token, bob.Session.ExpiresAt)
			origin, err := url.Parse(s.Config.PublicURL)
			if err != nil {
				t.Fatal(err)
			}
			for _, response := range delayed {
				t.Run(response.method+response.path, func(t *testing.T) {
					jar, err := cookiejar.New(nil)
					if err != nil {
						t.Fatal(err)
					}
					jar.SetCookies(origin, login.Result().Cookies())
					jar.SetCookies(origin, response.cookies)
					r := httptest.NewRequest("GET", "/api/v1/user/profile", nil)
					for _, cookie := range jar.Cookies(origin) {
						r.AddCookie(cookie)
					}
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					var profile struct {
						Code string
						Data auth.Profile
					}
					if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || w.Code != http.StatusOK || profile.Code != "OK" || profile.Data.ID != bob.Session.UserID {
						t.Fatalf("late %s response replaced the newly authenticated session: HTTP %d %s; decode error %v", invalidation, w.Code, w.Body.String(), err)
					}
				})
			}
			for _, query := range []string{"SELECT count(*) FROM orders", "SELECT count(*) FROM auth_flows WHERE intent='bind'"} {
				var count int
				if err := a.Store.DB.QueryRow(query).Scan(&count); err != nil || count != 0 {
					t.Fatalf("unusable session performed a protected write: count=%d error=%v", count, err)
				}
			}
		})
	}
}
