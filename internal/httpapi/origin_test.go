package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
)

func TestBrowserCanonicalOriginStartsLoginAndAcceptsAuthenticatedPOST(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", config.Example(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_SECRET", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("LOG_OUTPUTS", "console")
	t.Setenv("GEO_MODE", "disabled")
	t.Setenv("QQ_APP_ID", "local-client")
	t.Setenv("QQ_APP_SECRET", "local-secret")
	for _, tc := range []struct{ name, configured, origin, environment string }{
		{"canonical", "https://donate.example.com", "https://donate.example.com", "production"},
		{"uppercase", "https://DONATE.example.com", "https://donate.example.com", "production"},
		{"default-https-port", "https://donate.example.com:443", "https://donate.example.com", "production"},
		{"leading-zero-default-port", "https://donate.example.com:00443", "https://donate.example.com", "production"},
		{"uppercase-default-port", "https://DONATE.example.com:443", "https://donate.example.com", "production"},
		{"custom-port", "https://donate.example.com:8443", "https://donate.example.com:8443", "production"},
		{"leading-zero-custom-port", "https://donate.example.com:08443", "https://donate.example.com:8443", "production"},
		{"development-http80", "http://LOCALHOST:80", "http://localhost", "development"},
		{"leading-zero-http80", "http://LOCALHOST:00080", "http://localhost", "development"},
		{"underscore-host", "https://_donate.example", "https://_donate.example", "production"},
		{"hyphen-host", "https://ab--cd.example", "https://ab--cd.example", "production"},
		{"leading-hyphen-host", "https://-donate.example", "https://-donate.example", "production"},
		{"dns-trailing-dot", "https://DONATE.example.", "https://donate.example.", "production"},
		{"ipv4", "http://127.0.0.1:80", "http://127.0.0.1", "development"},
		{"ipv6", "https://[2001:0DB8:0:0:0:0:0:1]:443", "https://[2001:db8::1]", "production"},
		{"ipv6-custom-port", "https://[::1]:8443", "https://[::1]:8443", "production"},
		{"ipv6-mapped", "https://[::ffff:192.0.2.1]:443", "https://[::ffff:c000:201]", "production"},
		{"unicode-idn", "https://例子.测试:443", "https://xn--fsqu00a.xn--0zwm56d", "production"},
		{"unicode-dot", "https://例子。测试", "https://xn--fsqu00a.xn--0zwm56d", "production"},
		{"nontransitional-idn", "https://FAß.example", "https://xn--fa-hia.example", "production"},
		{"unicode-normalization", "https://BÜCHER.example", "https://xn--bcher-kva.example", "production"},
		{"ascii-idn", "https://XN--BCHER-KVA.example:443", "https://xn--bcher-kva.example", "production"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.environment)
			t.Setenv("PUBLIC_URL", tc.configured)
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.PublicURL != tc.configured {
				t.Fatalf("callback URL was rewritten: %q", cfg.PublicURL)
			}
			s, h, a := setup(t)
			s.Config, a.Config = cfg, cfg
			origin, _ := url.Parse(tc.origin)
			jar, _ := cookiejar.New(nil)
			site := httptest.NewRecorder()
			h.ServeHTTP(site, httptest.NewRequest("GET", "/api/v1/site/info", nil))
			jar.SetCookies(origin, site.Result().Cookies())
			var info struct {
				Data struct {
					CSRF string `json:"csrf_token"`
				}
			}
			if err := json.Unmarshal(site.Body.Bytes(), &info); err != nil {
				t.Fatal(err)
			}
			guest := httptest.NewRequest("POST", "/api/v1/user/login", strings.NewReader(`{"provider":"qq","return_to":"/"}`))
			guest.Header.Set("Origin", tc.origin)
			guest.Header.Set("Sec-Fetch-Site", "same-origin")
			guest.Header.Set("X-CSRF-Token", info.Data.CSRF)
			for _, c := range jar.Cookies(origin) {
				guest.AddCookie(c)
			}
			login := httptest.NewRecorder()
			h.ServeHTTP(login, guest)
			var flow struct {
				Code string
				Data struct {
					URL string `json:"authorization_url"`
				}
			}
			if err := json.Unmarshal(login.Body.Bytes(), &flow); err != nil {
				t.Fatal(err)
			}
			if login.Code != 200 || flow.Code != "OK" {
				t.Errorf("anonymous same-origin login: %d %s", login.Code, login.Body)
			} else {
				authorize, err := url.Parse(flow.Data.URL)
				if err != nil {
					t.Fatal(err)
				}
				if got := authorize.Query().Get("redirect_uri"); got != tc.configured+"/api/v1/user/oauth/callback/qq" {
					t.Fatalf("registered redirect_uri changed: %q", got)
				}
			}
			cred, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "local", Subject: tc.name})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/api/v1/user/session/renew", strings.NewReader(`{}`))
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("X-CSRF-Token", cred.Session.CSRF)
			r.AddCookie(&http.Cookie{Name: s.sessionCookieName(cred.Session.ID), Value: cred.Token})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":"OK"`) {
				t.Errorf("authenticated same-origin POST: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestCanonicalOriginStillRejectsForeignOrMalformedOriginAndBadCSRF(t *testing.T) {
	s, h, a := setup(t)
	s.Config.PublicURL, a.Config.PublicURL = "https://DONATE.example.com:443", "https://DONATE.example.com:443"
	a.Config.QQappID, a.Config.QQsecret = "local-client", "local-secret"
	cred, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "local", Subject: "origin-guard"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, origin, fetchSite string
		badCSRF                 bool
	}{
		{"missing", "", "same-origin", false},
		{"opaque", "null", "same-origin", false},
		{"foreign", "https://evil.example", "same-origin", false},
		{"different-scheme", "http://donate.example.com", "same-origin", false},
		{"different-port", "https://donate.example.com:8443", "same-origin", false},
		{"host-suffix", "https://donate.example.com.evil", "same-origin", false},
		{"path", "https://donate.example.com/", "same-origin", false},
		{"userinfo", "https://user@donate.example.com", "same-origin", false},
		{"query", "https://donate.example.com?x", "same-origin", false},
		{"fragment", "https://donate.example.com#x", "same-origin", false},
		{"cross-site", "https://donate.example.com", "cross-site", false},
		{"bad-token", "https://donate.example.com", "same-origin", true},
	} {
		for _, signedIn := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/anonymous", true: "/authenticated"}[signedIn], func(t *testing.T) {
				path, body, csrf := "/api/v1/user/login", `{"provider":"qq"}`, s.anonymousCSRF(strings.Repeat("a", 43))
				if signedIn {
					path, body, csrf = "/api/v1/user/session/renew", `{}`, cred.Session.CSRF
				}
				if tc.badCSRF {
					csrf = "invalid"
				}
				r := httptest.NewRequest("POST", path, strings.NewReader(body))
				r.Header.Set("Origin", tc.origin)
				r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
				r.Header.Set("X-CSRF-Token", csrf)
				if signedIn {
					r.AddCookie(&http.Cookie{Name: s.sessionCookieName(cred.Session.ID), Value: cred.Token})
				} else {
					r.AddCookie(&http.Cookie{Name: s.cookieName("csrf"), Value: strings.Repeat("a", 43)})
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != 403 || !strings.Contains(w.Body.String(), `"code":"CSRF_FAILED"`) {
					t.Fatalf("untrusted write accepted: %d %s", w.Code, w.Body)
				}
			})
		}
	}
}
