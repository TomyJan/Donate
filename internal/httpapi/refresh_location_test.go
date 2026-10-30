package httpapi

import (
	"context"
	"encoding/json"
	"github.com/TomyJan/Donate/internal/admin"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRefreshLocationHTTPConfirmationAuthorization(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	s.Admin.Config.GeoMode = "online"
	login := func(subject string) auth.Credentials {
		t.Helper()
		c, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "synthetic", Subject: subject})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	administrator, outsider := login("administrator"), login("outsider")
	s.Config.AdminIDs[administrator.Session.UserID] = true
	o, err := s.Orders.Create(ctx, order.CreateInput{UserID: administrator.Session.UserID, AmountFen: 1, Channel: "alipay", Key: "refresh-http", IP: "1.1.1.1"})
	if err != nil {
		t.Fatal(err)
	}
	post := func(path, body string, c auth.Credentials) (int, string, admin.Operation) {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Origin", s.Config.PublicURL)
		r.Header.Set("Content-Type", "application/json")
		if c.Token != "" {
			r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
			r.Header.Set("X-CSRF-Token", c.Session.CSRF)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var response struct {
			Code string
			Data admin.Operation
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return w.Code, response.Code, response.Data
	}
	body := `{"action":"refresh_location","ids":["` + o.ID + `"]}`
	for _, tc := range []struct {
		name   string
		c      auth.Credentials
		status int
		code   string
	}{{"guest", auth.Credentials{}, 401, "UNAUTHORIZED"}, {"outsider", outsider, 403, "FORBIDDEN"}} {
		t.Run(tc.name, func(t *testing.T) {
			status, code, _ := post("/api/v1/admin/order/action/preview", body, tc.c)
			if status != tc.status || code != tc.code {
				t.Fatal(status, code)
			}
		})
	}
	status, code, preview := post("/api/v1/admin/order/action/preview", body, administrator)
	if status != 200 || code != "OK" || len(preview.Items) != 1 || preview.Items[0].Result != "ready" {
		t.Fatal(status, code, preview)
	}
	confirm := `{"operation_id":"` + preview.ID + `"}`
	for _, tc := range []struct {
		c      auth.Credentials
		status int
	}{{auth.Credentials{}, 401}, {outsider, 403}} {
		status, _, _ := post("/api/v1/admin/order/action/execute", confirm, tc.c)
		if status != tc.status {
			t.Fatal(status)
		}
	}
	status, code, result := post("/api/v1/admin/order/action/execute", confirm, administrator)
	if status != 200 || code != "OK" || !result.Confirmed || len(result.Items) != 1 || result.Items[0].Result != "queued" || result.Items[0].JobID == "" {
		t.Fatal(status, code, result)
	}
	status, code, again := post("/api/v1/admin/order/action/execute", confirm, administrator)
	if status != 200 || code != "OK" || again.Items[0].JobID != result.Items[0].JobID {
		t.Fatal(status, code, again)
	}
	s.Config.AdminIDs[administrator.Session.UserID] = false
	status, code, _ = post("/api/v1/admin/order/action/execute", confirm, administrator)
	if status != 403 || code != "FORBIDDEN" {
		t.Fatal("revoked administrator could replay", status, code)
	}
}
