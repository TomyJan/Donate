package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
)

func httpIdentityFixture(t *testing.T) (*Server, http.Handler, auth.Credentials, auth.Credentials, auth.Credentials) {
	t.Helper()
	s, h, a := setup(t)
	ctx := context.Background()
	admin, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	s.Config.AdminIDs[admin.Session.UserID] = true
	member, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "member"})
	if err != nil {
		t.Fatal(err)
	}
	qq := auth.Identity{Provider: "qq", Issuer: "test", Subject: "member-qq", QQNumber: "123456"}
	if err = a.BindIdentity(ctx, member.Session.ID, qq); err != nil {
		t.Fatal(err)
	}
	other, err := a.LoginIdentity(ctx, qq)
	if err != nil {
		t.Fatal(err)
	}
	return s, h, admin, member, other
}

func identityRequest(s *Server, h http.Handler, c *auth.Credentials, path, body, origin, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", csrf)
	if c != nil {
		r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestIdentityUnbindRouteAuthorizationAndBusinessCodes(t *testing.T) {
	s, h, admin, member, _ := httpIdentityFixture(t)
	path := "/api/v1/admin/user/identity/unbind"
	body := fmt.Sprintf(`{"id":%q,"identity_id":%q}`, member.Session.UserID, member.Session.IdentityID)
	cases := []struct {
		name               string
		c                  *auth.Credentials
		body, origin, csrf string
		status             int
		code               string
	}{
		{"guest", nil, body, s.Config.PublicURL, "", 401, "UNAUTHORIZED"},
		{"member", &member, body, s.Config.PublicURL, member.Session.CSRF, 403, "FORBIDDEN"},
		{"csrf missing", &admin, body, s.Config.PublicURL, "", 403, "CSRF_FAILED"},
		{"wrong origin", &admin, body, "https://attacker.test", admin.Session.CSRF, 403, "CSRF_FAILED"},
		{"invalid UUID", &admin, `{"id":"bad","identity_id":"bad"}`, s.Config.PublicURL, admin.Session.CSRF, 200, "VALIDATION_FAILED"},
		{"mismatch", &admin, fmt.Sprintf(`{"id":%q,"identity_id":%q}`, admin.Session.UserID, member.Session.IdentityID), s.Config.PublicURL, admin.Session.CSRF, 200, "NOT_FOUND"},
		{"last identity", &admin, fmt.Sprintf(`{"id":%q,"identity_id":%q}`, admin.Session.UserID, admin.Session.IdentityID), s.Config.PublicURL, admin.Session.CSRF, 200, "LAST_IDENTITY_FORBIDDEN"},
		{"success", &admin, body, s.Config.PublicURL, admin.Session.CSRF, 200, "OK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := identityRequest(s, h, tc.c, path, tc.body, tc.origin, tc.csrf)
			var env Envelope
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatalf("HTTP %d %s: %v", w.Code, w.Body, err)
			}
			if w.Code != tc.status || env.Code != tc.code {
				t.Fatalf("HTTP %d %s", w.Code, w.Body)
			}
		})
	}
	if _, err := s.Auth.Resolve(context.Background(), member.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal("unbound session survived", err)
	}
	w := identityRequest(s, h, &admin, "/api/v1/user/identity/unbind", body, s.Config.PublicURL, admin.Session.CSRF)
	if w.Code != 404 && w.Code != 405 {
		t.Fatalf("self-service unbind route exposed: HTTP %d %s", w.Code, w.Body)
	}
}

func TestAdminUnbindingCurrentIdentityReturnsSuccessAndClearsCookie(t *testing.T) {
	s, h, _, member, other := httpIdentityFixture(t)
	s.Config.AdminIDs[member.Session.UserID] = true
	body := fmt.Sprintf(`{"id":%q,"identity_id":%q}`, strings.ToUpper(member.Session.UserID), strings.ToUpper(member.Session.IdentityID))
	w := identityRequest(s, h, &member, "/api/v1/admin/user/identity/unbind", body, s.Config.PublicURL, member.Session.CSRF)
	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || env.Code != "OK" {
		t.Fatalf("committed self-unbind reported failure: %d %s", w.Code, w.Body)
	}
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.Name == s.sessionName() && c.Value == "" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("current session cookie not cleared")
	}
	if _, err := s.Auth.Resolve(context.Background(), member.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err := s.Auth.Resolve(context.Background(), other.Token); err != nil {
		t.Fatal("remaining login method lost", err)
	}
}
