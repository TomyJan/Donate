package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
)

func TestQQConfirmationErrorsDoNotInvalidateExistingLogin(t *testing.T) {
	s, h, a := setup(t)
	a.Config.QQappID = "app"
	c, err := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	flow, err := a.BeginFlow(context.Background(), "qq", "bind", &c.Session, "/")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		number, code string
		expired      bool
	}{
		{"123abc", "QQ_NUMBER_INVALID", false},
		{"123456", "AUTH_FLOW_EXPIRED", true},
	} {
		if tc.expired {
			if _, err := a.Store.DB.Exec("UPDATE auth_flows SET expires_at=0"); err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest("POST", "/api/v1/user/login/qq/complete", strings.NewReader(`{"qq_number":"`+tc.number+`"}`))
		r.Header.Set("Origin", s.Config.PublicURL)
		r.Header.Set("X-CSRF-Token", c.Session.CSRF)
		r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
		r.AddCookie(&http.Cookie{Name: s.cookieName("flow"), Value: flow.Cookie})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		for _, sensitive := range []string{"头像", "image_mismatch", tc.number, "qlogo"} {
			if strings.Contains(w.Body.String(), sensitive) {
				t.Fatalf("verification details leaked: %s", w.Body)
			}
		}
		if _, err := a.Resolve(context.Background(), c.Token); err != nil {
			t.Fatalf("existing login lost: %v", err)
		}
	}
}

func TestOAuthRejectionsRedirectToClassifiedSafePage(t *testing.T) {
	for _, tc := range []struct{ name, providerError, state, want string }{
		{"cancelled", "access_denied", "", "cancelled"},
		{"rejected", "untrusted-private-provider-error", "", "verification"},
		{"wrong state", "access_denied", "wrong-state", "flow_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, h, a := setup(t)
			a.Config.QQappID = "app"
			flow, err := a.BeginFlow(context.Background(), "qq", "login", nil, "/")
			if err != nil {
				t.Fatal(err)
			}
			state := flow.State
			if tc.state != "" {
				state = tc.state
			}
			q := url.Values{"state": {state}, "error": {tc.providerError}, "error_description": {"private-token"}}
			r := httptest.NewRequest("GET", "/api/v1/user/oauth/callback/qq?"+q.Encode(), nil)
			r.AddCookie(&http.Cookie{Name: s.cookieName("flow"), Value: flow.Cookie})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 303 || w.Header().Get("Location") != "/auth/complete?error="+tc.want {
				t.Fatalf("%d %s", w.Code, w.Header().Get("Location"))
			}
			if strings.Contains(w.Body.String()+w.Header().Get("Location"), "private") {
				t.Fatal("provider text exposed")
			}
		})
	}
}
