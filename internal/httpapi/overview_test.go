package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/admin"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestAdminOverviewRangeContract(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	login, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "overview"})
	if err != nil {
		t.Fatal(err)
	}
	s.Config.AdminIDs[login.Session.UserID] = true
	// At this UTC instant it is already October 31 in Shanghai.
	s.Admin.Now = func() time.Time { return time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC) }
	for i, paid := range []string{"2026-07-31T23:59:59+08:00", "2026-08-03T00:00:00+08:00", "2026-10-02T00:00:00+08:00", "2026-10-25T00:00:00+08:00", "2026-10-30T23:59:59+08:00", "2026-10-31T00:00:00+08:00", "2026-10-31T23:59:59+08:00", "2026-11-01T00:00:00+08:00"} {
		o, err := s.Orders.Create(ctx, order.CreateInput{UserID: login.Session.UserID, Key: paid, AmountFen: 100, Channel: "alipay"})
		if err != nil {
			t.Fatal(err)
		}
		at, err := time.Parse(time.RFC3339, paid)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := storage.ParseID(o.ID)
		if _, err = s.Orders.Store.DB.ExecContext(ctx, "UPDATE orders SET status='paid',created_at=?,paid_at=?,trade_no=? WHERE id=?", at.Unix(), at.Unix(), paid, id); err != nil {
			t.Fatal(i, err)
		}
	}
	for _, tc := range []struct {
		name, query string
		paid        int
		code        string
	}{
		{"default", "", 5, "OK"}, {"today", "range=today", 2, "OK"}, {"yesterday", "range=yesterday", 1, "OK"},
		{"week", "range=7d", 4, "OK"}, {"month", "range=30d", 5, "OK"}, {"quarter", "range=90d", 6, "OK"}, {"all", "range=all", 8, "OK"},
		{"custom", "range=custom&from=" + url.QueryEscape("2026-10-30T00:00:00+08:00") + "&to=" + url.QueryEscape("2026-10-31T00:00:00+08:00"), 1, "OK"},
		{"fractional bounds", "range=custom&from=" + url.QueryEscape("2026-10-31T00:00:00.5+08:00") + "&to=" + url.QueryEscape("2026-10-31T23:59:59.5+08:00"), 1, "OK"},
		{"empty result", "range=custom&from=2025-01-01T00:00:00Z&to=2025-01-02T00:00:00Z", 0, "OK"},
		{"empty custom", "range=custom", 0, "VALIDATION_FAILED"}, {"missing to", "range=custom&from=2026-01-01T00:00:00Z", 0, "VALIDATION_FAILED"},
		{"missing from", "range=custom&to=2026-01-01T00:00:00Z", 0, "VALIDATION_FAILED"},
		{"bad date", "range=custom&from=oops&to=2026-02-01T00:00:00Z", 0, "VALIDATION_FAILED"},
		{"equal", "range=custom&from=2026-01-01T00:00:00Z&to=2026-01-01T00:00:00Z", 0, "VALIDATION_FAILED"},
		{"reversed", "range=custom&from=2026-02-01T00:00:00Z&to=2026-01-01T00:00:00Z", 0, "VALIDATION_FAILED"},
		{"unknown", "range=forever", 0, "VALIDATION_FAILED"}, {"SQL range", "range=30d%27%3BDROP%20TABLE%20orders", 0, "VALIDATION_FAILED"},
		{"dates without custom", "from=2026-01-01T00:00:00Z", 0, "VALIDATION_FAILED"}, {"all with dates", "range=all&to=2026-01-01T00:00:00Z", 0, "VALIDATION_FAILED"},
		{"empty date outside custom", "range=today&from=", 0, "VALIDATION_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/admin/overview?"+tc.query, nil)
			req.AddCookie(&http.Cookie{Name: s.sessionName(), Value: login.Token})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			var body struct {
				Code string
				Data admin.Overview
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || body.Code != tc.code {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.code != "OK" {
				return
			}
			if body.Data.Paid != tc.paid || body.Data.PaidFen != int64(tc.paid*100) {
				t.Fatalf("overview=%+v, want %d paid", body.Data, tc.paid)
			}
			for _, metrics := range [][]admin.Metric{body.Data.Daily, body.Data.Channels} {
				var amount int64
				var count int
				for _, m := range metrics {
					amount += m.PaidFen
					count += m.Count
				}
				if count != tc.paid || amount != int64(tc.paid*100) {
					t.Errorf("inconsistent metrics=%+v", metrics)
				}
			}
		})
	}
}
