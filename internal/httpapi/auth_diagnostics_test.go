package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/go-chi/chi/v5"
)

func TestHTTPPanicLogIdentifiesCallsiteWithoutExposingPanicValue(t *testing.T) {
	for _, value := range []any{
		"private-panic-token",
		&url.Error{Op: "Get", URL: "https://private-host/?key=private-key", Err: errors.New("private-body")},
	} {
		s, _, _ := setup(t)
		var logs bytes.Buffer
		s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
		router := chi.NewRouter()
		router.Use(s.middleware)
		router.Get("/broken/{value}", func(http.ResponseWriter, *http.Request) { panic(value) })
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/broken/private-path?token=private-query", nil))
		if w.Code != http.StatusInternalServerError || strings.Contains(logs.String()+w.Body.String(), "private-") {
			t.Fatalf("invalid response or secret exposed: %d %s %s", w.Code, w.Body.String(), logs.String())
		}
		found := false
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatal(err)
			}
			if entry["stage"] != "http_panic" {
				continue
			}
			stack, _ := entry["stack"].(string)
			if regexp.MustCompile(`(?m)\([^\n]*\)$`).MatchString(stack) {
				t.Fatal("stack includes call arguments")
			}
			if entry["request_id"] != w.Header().Get("X-Request-ID") || entry["route"] != "/broken/{value}" || entry["panic_type"] == nil || entry["duration_ms"] == nil || !strings.Contains(stack, "auth_diagnostics_test.go") || !strings.Contains(stack, "server.go") {
				t.Fatalf("missing panic context: %v", entry)
			}
			found = true
		}
		if !found {
			t.Fatalf("panic diagnostic missing: %s", logs.String())
		}
	}
}

func TestHTTPFailureDiagnosticRedactsArbitraryErrorText(t *testing.T) {
	s, _, _ := setup(t)
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	router := chi.NewRouter()
	router.Use(s.middleware)
	router.Get("/broken", func(w http.ResponseWriter, r *http.Request) {
		s.failure(w, r, errors.New("private-provider-body private-token https://private-user:private-password@issuer.test/path?secret=private-query"))
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/broken", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("failure response=%d", w.Code)
	}
	if strings.Contains(logs.String(), "private-") {
		t.Fatalf("raw error exposed: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"reason":"internal_error"`) {
		t.Fatalf("missing safe error reason: %s", logs.String())
	}
}

func TestOAuthFailureLogsCorrelateHTTPAndAuthWithoutSecrets(t *testing.T) {
	s, h, a := setup(t)
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	a.Log = s.Log
	r := httptest.NewRequest(http.MethodGet, "/api/v1/user/oauth/callback/oidc?state=private-state&code=private-code", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("callback response changed: %d", w.Code)
	}
	id := w.Header().Get("X-Request-ID")
	var foundAuth, foundHTTP bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e["stage"] == "flow_read" && e["reason"] == "invalid_cookie" && e["request_id"] == id && strings.Contains(e["msg"].(string), id) {
			foundAuth = true
		}
		if e["route"] == "/api/v1/user/oauth/callback/{provider}" && e["request_id"] == id && e["code"] == "AUTHORIZATION_FAILED" {
			foundHTTP = true
		}
	}
	if id == "" || !foundAuth || !foundHTTP {
		t.Fatalf("unrelated/missing auth and HTTP diagnostics: %s", logs.String())
	}
	if strings.Contains(logs.String(), "private-") {
		t.Fatalf("authorization query exposed: %s", logs.String())
	}
}

func TestExpiredSessionReasonIsDebugAtHTTPBoundary(t *testing.T) {
	s, h, a := setup(t)
	c, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.DB.Exec("UPDATE sessions SET expires_at=0"); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a.Log = s.Log
	r := httptest.NewRequest(http.MethodGet, "/api/v1/user/profile", nil)
	r.AddCookie(&http.Cookie{Name: s.sessionCookieName(c.Session.ID), Value: c.Token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d", w.Code)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e["stage"] == "session_resolve" && e["reason"] == "session_expired" && e["request_id"] == w.Header().Get("X-Request-ID") && e["level"] == "DEBUG" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing quiet expired session reason: %s", logs.String())
	}
	if strings.Contains(logs.String(), c.Token) {
		t.Fatal("session token exposed")
	}
}

func TestRoutineReadCompletionsRequireDebugLogging(t *testing.T) {
	for _, path := range []string{"/health/live", "/api/v1/site/info", "/api/v1/user/profile", "/api/v1/admin/task/list", "/api/v1/admin/task/overview", "/"} {
		t.Run(path, func(t *testing.T) {
			s, h, a := setup(t)
			c, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "admin"})
			if err != nil {
				t.Fatal(err)
			}
			s.Config.AdminIDs[c.Session.UserID] = true
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
			a.Log = s.Log
			request := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodGet, path, nil)
				r.AddCookie(&http.Cookie{Name: s.sessionCookieName(c.Session.ID), Value: c.Token})
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			if w := request(); w.Code != 200 {
				t.Fatalf("routine read failed: %d %s", w.Code, w.Body.String())
			}
			if logs.Len() != 0 {
				t.Fatalf("routine polling floods INFO logs: %s", logs.String())
			}
			s.Log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			a.Log = s.Log
			request()
			found := false
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var e map[string]any
				if err := json.Unmarshal([]byte(line), &e); err != nil {
					t.Fatal(err)
				}
				if _, ok := e["route"]; ok && e["level"] == "DEBUG" {
					found = true
				}
			}
			if !found {
				t.Fatalf("routine completion missing at DEBUG: %s", logs.String())
			}
		})
	}
}
