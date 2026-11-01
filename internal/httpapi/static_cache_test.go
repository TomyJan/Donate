package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
)

// The intermediary deliberately shares explicitly public responses, including
// their Set-Cookie fields. Cookie issuance must disable that public policy.
func publicAssetCache(origin http.Handler) http.Handler {
	type cachedResponse struct {
		header http.Header
		status int
		body   []byte
	}
	var mu sync.Mutex
	cache := map[string]cachedResponse{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		key := r.Method + " " + r.URL.RequestURI()
		response, hit := cache[key]
		if !hit {
			recorded := httptest.NewRecorder()
			origin.ServeHTTP(recorded, r)
			response = cachedResponse{recorded.Result().Header.Clone(), recorded.Code, append([]byte(nil), recorded.Body.Bytes()...)}
			policy := response.header.Get("Cache-Control")
			if r.Method == http.MethodGet && strings.Contains(policy, "public") && strings.Contains(policy, "max-age=") && !strings.Contains(policy, "private") && !strings.Contains(policy, "no-store") {
				cache[key] = response
			}
		}
		for name, values := range response.header {
			w.Header()[name] = append([]string(nil), values...)
		}
		if hit {
			w.Header().Set("X-Test-Cache", "hit")
		} else {
			w.Header().Set("X-Test-Cache", "miss")
		}
		w.WriteHeader(response.status)
		_, _ = w.Write(response.body)
	})
}

func TestStaticCacheCannotReplaySessionCookies(t *testing.T) {
	for _, mode := range []string{"legacy-migration", "browser-anchor-repair", "session-renewal"} {
		t.Run(mode, func(t *testing.T) {
			s, h, a := setup(t)
			s.Config.Development, a.Config.Development = false, false
			s.Assets = fstest.MapFS{"assets/cache-test.js": {Data: []byte("console.log('cache-test')")}}
			origin := httptest.NewTLSServer(publicAssetCache(h))
			defer origin.Close()
			s.Config.PublicURL, a.Config.PublicURL = origin.URL, origin.URL
			base, err := url.Parse(origin.URL)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Second)
			a.Now = func() time.Time { return now }
			owner, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "cache-test", Subject: mode})
			if err != nil {
				t.Fatal(err)
			}
			client := func() *http.Client {
				jar, err := cookiejar.New(nil)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Client{Transport: origin.Client().Transport, Jar: jar}
			}
			member, anonymous := client(), client()
			initial := httptest.NewRecorder()
			if mode == "legacy-migration" {
				s.setCookie(initial, s.sessionName(), owner.Token, owner.Session.ExpiresAt)
			} else if mode == "browser-anchor-repair" {
				s.setCookie(initial, s.sessionCookieName(owner.Session.ID), owner.Token, owner.Session.ExpiresAt)
			} else {
				s.writeSessionCookie(initial, httptest.NewRequest("GET", "/", nil), owner.Token, owner.Session)
			}
			member.Jar.SetCookies(base, initial.Result().Cookies())
			if mode == "session-renewal" {
				now = now.Add(10 * 24 * time.Hour)
				req, err := http.NewRequest("POST", origin.URL+"/api/v1/user/session/renew", strings.NewReader("{}"))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Origin", origin.URL)
				req.Header.Set("X-CSRF-Token", owner.Session.CSRF)
				res, err := member.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, res.Body)
				res.Body.Close()
				renewed := false
				for _, cookie := range res.Cookies() {
					if cookie.Name == s.sessionCookieName(owner.Session.ID) && cookie.Value == owner.Token && cookie.Expires.Equal(now.Add(30*24*time.Hour)) {
						renewed = true
					}
				}
				if res.StatusCode != 200 || !renewed || !strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
					t.Fatalf("renewal status=%d renewed=%v policy=%q", res.StatusCode, renewed, res.Header.Get("Cache-Control"))
				}
			}
			asset := origin.URL + "/assets/cache-test.js"
			res, err := member.Get(asset)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 || string(body) != "console.log('cache-test')" {
				t.Fatalf("asset status=%d body=%q err=%v", res.StatusCode, body, err)
			}
			if mode != "session-renewal" {
				policy := res.Header.Get("Cache-Control")
				if len(res.Cookies()) == 0 || !strings.Contains(policy, "private") || !strings.Contains(policy, "no-store") {
					t.Errorf("cookie-issuing asset policy=%q cookies=%d", policy, len(res.Cookies()))
				}
			} else if len(res.Cookies()) != 0 {
				t.Fatalf("renewed member asset unexpectedly issued cookies: %+v", res.Cookies())
			}
			for range 2 {
				res, err = anonymous.Get(asset)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, res.Body)
				res.Body.Close()
			}
			profile, err := anonymous.Get(origin.URL + "/api/v1/user/profile")
			if err != nil {
				t.Fatal(err)
			}
			profileBody, err := io.ReadAll(profile.Body)
			profile.Body.Close()
			if err != nil || profile.StatusCode != http.StatusUnauthorized {
				t.Errorf("shared asset response authenticated anonymous jar: status=%d body=%s err=%v", profile.StatusCode, profileBody, err)
			}
			if res.StatusCode != 200 || res.Header.Get("X-Test-Cache") != "hit" || res.Header.Get("Cache-Control") != "public,max-age=31536000,immutable" || len(res.Cookies()) != 0 {
				t.Errorf("anonymous asset cache: status=%d cache=%q policy=%q cookies=%d", res.StatusCode, res.Header.Get("X-Test-Cache"), res.Header.Get("Cache-Control"), len(res.Cookies()))
			}
			profile, err = member.Get(origin.URL + "/api/v1/user/profile")
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct{ Data struct{ ID string } }
			err = json.NewDecoder(profile.Body).Decode(&envelope)
			profile.Body.Close()
			if err != nil || profile.StatusCode != 200 || envelope.Data.ID != owner.Session.UserID {
				t.Fatalf("member lost session: status=%d id=%q err=%v", profile.StatusCode, envelope.Data.ID, err)
			}
		})
	}
}
