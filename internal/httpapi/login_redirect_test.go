package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestLoginGETRedirectsToProviderAndPreservesReturnTarget(t *testing.T) {
	s, h, a := setup(t)
	a.Config.QQappID = "123"
	target := "/?payment=returned&order=01234567-89ab-cdef-0123-456789abcdef"
	r := httptest.NewRequest("GET", "/api/v1/user/login?"+url.Values{"provider": {"qq"}, "return_to": {target}}.Encode(), nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.Header.Set("Sec-Fetch-Dest", "document")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("login navigation: %d %s", w.Code, w.Body)
	}
	destination, err := url.Parse(w.Header().Get("Location"))
	if err != nil || destination.Host != "graph.qq.com" || destination.Query().Get("client_id") != "123" || destination.Query().Get("state") == "" {
		t.Fatalf("OAuth location: %s %v", w.Header().Get("Location"), err)
	}
	var flowCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == s.cookieName("flow") {
			flowCookie = c
		}
	}
	if flowCookie == nil || !flowCookie.HttpOnly || flowCookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("browser-bound flow cookie missing")
	}
	status := httptest.NewRequest("GET", "/api/v1/user/login/status", nil)
	status.AddCookie(flowCookie)
	statusResponse := httptest.NewRecorder()
	h.ServeHTTP(statusResponse, status)
	if !strings.Contains(statusResponse.Body.String(), "payment=returned") || !strings.Contains(statusResponse.Body.String(), "01234567-89ab-cdef-0123-456789abcdef") {
		t.Fatalf("return target lost: %s", statusResponse.Body)
	}
}

func TestLoginGETRejectsAmbiguousQueriesAndCrossSiteSubresources(t *testing.T) {
	cases := []struct {
		name, path, mode, dest string
		status                 int
		code                   string
	}{
		{"duplicate provider", "/api/v1/user/login?provider=qq&provider=oidc", "navigate", "document", 303, ""},
		{"duplicate return target", "/api/v1/user/login?provider=qq&return_to=/&return_to=/admin", "navigate", "document", 303, ""},
		{"cross-site fetch", "/api/v1/user/login?provider=qq", "cors", "empty", 403, "CSRF_FAILED"},
		{"cross-site iframe", "/api/v1/user/login?provider=qq", "navigate", "iframe", 403, "CSRF_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, h, a := setup(t)
			a.Config.QQappID = "123"
			r := httptest.NewRequest("GET", tc.path, nil)
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			r.Header.Set("Sec-Fetch-Mode", tc.mode)
			r.Header.Set("Sec-Fetch-Dest", tc.dest)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			wantLocation := ""
			if tc.status == http.StatusSeeOther {
				wantLocation = "/auth/complete?error=authorization"
			}
			if w.Header().Get("Location") != wantLocation {
				t.Fatalf("rejected request location: %q", w.Header().Get("Location"))
			}
		})
	}
}

func TestLoginGETNavigationFailuresUseSafeErrorPageAndKeepDiagnostics(t *testing.T) {
	for _, scenario := range []string{"provider unavailable", "rate limited", "provider disabled", "invalid query"} {
		t.Run(scenario, func(t *testing.T) {
			s, h, a := setup(t)
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
			a.Log = s.Log
			path, wantCode := "/api/v1/user/login?provider=qq", "VALIDATION_FAILED"
			switch scenario {
			case "provider unavailable":
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "private-provider-body private-token", http.StatusServiceUnavailable)
				}))
				t.Cleanup(provider.Close)
				a.Config.OIDCissuer, a.Config.OIDCclientID = provider.URL, "client"
				path, wantCode = "/api/v1/user/login?provider=oidc", "LOGIN_PROVIDER_UNAVAILABLE"
			case "rate limited":
				wantCode = "RATE_LIMITED"
				for range 15 {
					s.limits.allow("203.0.113.17:login", 15, time.Minute)
				}
			case "invalid query":
				path = "/api/v1/user/login?provider=qq&return_to=%ZZ"
			}
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.RemoteAddr = "203.0.113.17:1234"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/auth/complete?error=authorization" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("browser left on raw API error: %d %s %s", w.Code, w.Header(), w.Body)
			}
			if strings.Contains(w.Header().Get("Location")+w.Body.String()+logs.String(), "private-") {
				t.Fatal("upstream error text leaked")
			}
			matchedCompletion, matchedDiscovery := false, false
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if entry["request_id"] == w.Header().Get("X-Request-ID") && entry["code"] == wantCode && entry["status"] == float64(303) {
					matchedCompletion = true
				}
				if entry["stage"] == "oidc_discovery" && entry["request_id"] == w.Header().Get("X-Request-ID") {
					matchedDiscovery = true
				}
			}
			if !matchedCompletion || (scenario == "provider unavailable" && !matchedDiscovery) {
				t.Fatalf("server diagnostics lost: %s", logs.String())
			}
			for _, c := range w.Result().Cookies() {
				if c.Name == s.cookieName("flow") && c.Value != "" {
					t.Fatal("failed start issued a flow cookie")
				}
			}
		})
	}
}

func TestLoginGETCrossSiteSubresourcesStayForbiddenWhenRateLimited(t *testing.T) {
	s, h, _ := setup(t)
	for range 15 {
		s.limits.allow("203.0.113.17:login", 15, time.Minute)
	}
	for _, dest := range []string{"empty", "iframe"} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/user/login?provider=qq", nil)
		r.RemoteAddr = "203.0.113.17:1234"
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.Header.Set("Sec-Fetch-Dest", dest)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), "CSRF_FAILED") {
			t.Fatalf("subresource redirected instead of rejected: %d %s %s", w.Code, w.Header(), w.Body)
		}
	}
}

func TestLoginPOSTAndBindingKeepJSONBeginErrors(t *testing.T) {
	for _, path := range []string{"/api/v1/user/login", "/api/v1/user/identity/bind"} {
		t.Run(path, func(t *testing.T) {
			s, h, a := setup(t)
			current, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "legacy-errors"})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"provider":"qq","return_to":"/"}`))
			r.Header.Set("Origin", s.Config.PublicURL)
			r.Header.Set("X-CSRF-Token", current.Session.CSRF)
			r.AddCookie(&http.Cookie{Name: s.sessionCookieName(current.Session.ID), Value: current.Token})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), "VALIDATION_FAILED") || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("POST error contract changed: %d %s %s", w.Code, w.Header(), w.Body)
			}
		})
	}
}

func TestBannedOAuthCallbackExplainsFailureWithoutReplacingCurrentSession(t *testing.T) {
	s, h, a := setup(t)
	a.Config.QQappID = "123"
	a.Config.QQsecret = "secret"
	ctx := context.Background()
	banned, err := a.LoginIdentity(ctx, auth.Identity{Provider: "qq", Issuer: "123", Subject: "banned-subject", QQNumber: "12345"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := storage.ParseID(banned.Session.UserID)
	if _, err = a.Store.DB.Exec("UPDATE users SET status='banned' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	current, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "current-account"})
	if err != nil {
		t.Fatal(err)
	}
	a.HTTP = &http.Client{Transport: reauthenticationTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"ret":0,"nickname":"Private banned name"}`
		if r.URL.Path == "/oauth2.0/token" {
			body = `{"access_token":"token"}`
		}
		if r.URL.Path == "/oauth2.0/me" {
			body = `{"client_id":"123","openid":"banned-subject"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	start := httptest.NewRequest("GET", "/api/v1/user/login?provider=qq&return_to=%2F", nil)
	start.AddCookie(&http.Cookie{Name: s.sessionCookieName(current.Session.ID), Value: current.Token})
	started := httptest.NewRecorder()
	h.ServeHTTP(started, start)
	if started.Code != http.StatusFound {
		t.Fatalf("start banned login: %d %s", started.Code, started.Body)
	}
	authorization, err := url.Parse(started.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/user/oauth/callback/qq?"+url.Values{"state": {authorization.Query().Get("state")}, "code": {"code"}}.Encode(), nil)
	for _, c := range started.Result().Cookies() {
		if c.Name == s.cookieName("flow") {
			r.AddCookie(c)
		}
	}
	r.AddCookie(&http.Cookie{Name: s.sessionCookieName(current.Session.ID), Value: current.Token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 303 || w.Header().Get("Location") != "/auth/complete?error=account_banned" {
		t.Fatalf("banned callback: %d %s", w.Code, w.Header().Get("Location"))
	}
	if strings.Contains(w.Body.String(), "Private banned name") || strings.Contains(w.Header().Get("Location"), banned.Session.UserID) {
		t.Fatal("private identity leaked")
	}
	if _, err = a.Resolve(ctx, current.Token); err != nil {
		t.Fatalf("unrelated valid session revoked: %v", err)
	}
	for _, c := range w.Result().Cookies() {
		if strings.HasPrefix(c.Name, s.sessionName()) {
			t.Fatal("banned callback replaced session cookie")
		}
	}
}
