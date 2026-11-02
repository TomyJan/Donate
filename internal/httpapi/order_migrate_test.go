package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/storage"
)

// Removing the admin/CSRF boundary, accepting malformed payloads, or returning
// success without assigning the order must each fail this route-level test.
func TestOrderMigrateRouteAuthorizationValidationAndReplay(t *testing.T) {
	s, h, actor, member, _ := httpIdentityFixture(t)
	id := storage.NewID()
	if _, err := s.Orders.Store.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,status,created_at,expires_at,protocol,request_ip) VALUES(?,'migrate-http',888,'qqpay','paid',1640995200,1640997000,'legacy','192.0.2.1')", id[:]); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"id":%q,"user_id":%q}`, id.String(), member.Session.UserID)
	cases := []struct {
		name               string
		credentials        *auth.Credentials
		body, origin, csrf string
		status             int
		code, result       string
	}{
		{"guest", nil, body, s.Config.PublicURL, "", 401, "UNAUTHORIZED", ""},
		{"member", &member, body, s.Config.PublicURL, member.Session.CSRF, 403, "FORBIDDEN", ""},
		{"missing csrf", &actor, body, s.Config.PublicURL, "", 403, "CSRF_FAILED", ""},
		{"wrong origin", &actor, body, "https://attacker.test", actor.Session.CSRF, 403, "CSRF_FAILED", ""},
		{"invalid id", &actor, `{"id":"bad","user_id":"bad"}`, s.Config.PublicURL, actor.Session.CSRF, 200, "VALIDATION_FAILED", ""},
		{"missing target", &actor, fmt.Sprintf(`{"id":%q}`, id.String()), s.Config.PublicURL, actor.Session.CSRF, 200, "VALIDATION_FAILED", ""},
		{"unknown field", &actor, body[:len(body)-1] + `,"owner":"other"}`, s.Config.PublicURL, actor.Session.CSRF, 200, "VALIDATION_FAILED", ""},
		{"trailing payload", &actor, body + ` {}`, s.Config.PublicURL, actor.Session.CSRF, 200, "VALIDATION_FAILED", ""},
		{"missing user", &actor, fmt.Sprintf(`{"id":%q,"user_id":%q}`, id.String(), storage.NewID().String()), s.Config.PublicURL, actor.Session.CSRF, 200, "NOT_FOUND", ""},
		{"migrated", &actor, body, s.Config.PublicURL, actor.Session.CSRF, 200, "OK", "migrated"},
		{"replay", &actor, body, s.Config.PublicURL, actor.Session.CSRF, 200, "OK", "unchanged"},
		{"different owner", &actor, fmt.Sprintf(`{"id":%q,"user_id":%q}`, id.String(), actor.Session.UserID), s.Config.PublicURL, actor.Session.CSRF, 200, "ORDER_MIGRATION_NOT_ALLOWED", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := identityRequest(s, h, tc.credentials, "/api/v1/admin/order/migrate", tc.body, tc.origin, tc.csrf)
			var wire struct {
				Code string            `json:"code"`
				Data map[string]string `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
				t.Fatalf("HTTP %d %s: %v", w.Code, w.Body, err)
			}
			if w.Code != tc.status || wire.Code != tc.code {
				t.Fatalf("HTTP %d %s", w.Code, w.Body)
			}
			if tc.result != "" && (wire.Data["id"] != id.String() || wire.Data["user_id"] != member.Session.UserID || wire.Data["result"] != tc.result) {
				t.Fatalf("unexpected migration payload: %s", w.Body)
			}
		})
	}
	got, err := s.Orders.Get(context.Background(), id.String())
	if err != nil || got.UserID != member.Session.UserID {
		t.Fatalf("ownership was not committed: %+v %v", got, err)
	}
}

// Ownership must change the existing detail authorization immediately, while
// retaining the ordinary-user response shape and unrelated users' denial.
func TestOrderMigrateGrantsRecipientDetailAccessWithoutAdminFields(t *testing.T) {
	s, h, actor, member, _ := httpIdentityFixture(t)
	ctx := context.Background()
	unrelated, err := s.Auth.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "unrelated-viewer"})
	if err != nil {
		t.Fatal(err)
	}
	id, location := storage.NewID(), storage.NewID()
	if _, err := s.Orders.Store.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,status,created_at,expires_at,protocol,request_ip,donor_nickname,donor_avatar_url,donor_qq_number) VALUES(?,'migrate-detail',888,'qqpay','paid',1640995200,1640997000,'legacy','192.0.2.42','原始捐赠者','https://example.test/original.png','123456')", id[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Orders.Store.DB.Exec("INSERT INTO order_locations(id,order_id,province,street,source,queried_at) VALUES(?,?,'原始省份','管理员可见街道','legacy',1640995300)", location[:], id[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Orders.Store.DB.Exec("INSERT INTO order_location_checks(order_id,result_json) VALUES(?,?)", id[:], `{"province":"复查省份","source":"private-check"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Queue.EnqueueOnce(ctx, id.String(), actor.Session.UserID, "migration-detail-test", "detail-reconciliation"); err != nil {
		t.Fatal(err)
	}
	detail := func(credentials auth.Credentials, path string, wantStatus int, wantCode string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path+"?id="+id.String(), nil)
		r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: credentials.Token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var env struct {
			Code string         `json:"code"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("detail HTTP %d %s: %v", w.Code, w.Body, err)
		}
		if w.Code != wantStatus || env.Code != wantCode {
			t.Fatalf("detail HTTP %d %s", w.Code, w.Body)
		}
		return env.Data
	}
	const userPath = "/api/v1/order/detail"
	const adminPath = "/api/v1/admin/order/detail"
	detail(member, userPath, http.StatusForbidden, "FORBIDDEN")
	detail(unrelated, userPath, http.StatusForbidden, "FORBIDDEN")
	// Positive controls keep the privacy assertions meaningful: every protected
	// field is populated and present in the administrator's response.
	full := detail(actor, adminPath, http.StatusOK, "OK")
	for _, key := range []string{"qq_number", "location", "location_check", "jobs", "reconciliations"} {
		if _, exists := full[key]; !exists {
			t.Fatalf("administrator fixture is missing %s: %+v", key, full)
		}
	}
	memberID, _ := storage.ParseID(member.Session.UserID)
	if _, err := s.Orders.Store.DB.Exec("UPDATE identities SET nickname='现有用户',avatar_url='https://example.test/current.png' WHERE user_id=?", memberID); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"id":%q,"user_id":%q}`, id.String(), member.Session.UserID)
	w := identityRequest(s, h, &actor, "/api/v1/admin/order/migrate", body, s.Config.PublicURL, actor.Session.CSRF)
	var migrated struct {
		Code string `json:"code"`
		Data struct {
			Result string `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &migrated); err != nil || w.Code != http.StatusOK || migrated.Code != "OK" || migrated.Data.Result != "migrated" {
		t.Fatalf("migration HTTP %d %s: %v", w.Code, w.Body, err)
	}
	owned := detail(member, userPath, http.StatusOK, "OK")
	if owned["id"] != id.String() || owned["user_id"] != member.Session.UserID || owned["ip"] != "192.0.2.42" || owned["nickname"] != "现有用户" || owned["avatar_url"] != "https://example.test/current.png" || owned["amount_fen"] != float64(888) || owned["province"] != "原始省份" {
		t.Fatalf("recipient detail lost historical fields or ownership: %+v", owned)
	}
	for _, key := range []string{"qq_number", "location", "location_check", "jobs", "reconciliations"} {
		if _, exists := owned[key]; exists {
			t.Fatalf("recipient was given administrator field %s: %+v", key, owned)
		}
	}
	detail(member, adminPath, http.StatusForbidden, "FORBIDDEN")
	detail(unrelated, userPath, http.StatusForbidden, "FORBIDDEN")
}
