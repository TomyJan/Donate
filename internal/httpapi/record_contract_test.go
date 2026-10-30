package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestRecordOwnershipUsesSessionAndDetailKeepsPrivacy(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	login := func(subject string) auth.Credentials {
		t.Helper()
		c, e := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: subject, Nickname: subject})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	owner, other, administrator := login("owner"), login("other"), login("admin")
	s.Config.AdminIDs[administrator.Session.UserID] = true
	makeOrder := func(key, status string, hidden bool) order.Order {
		t.Helper()
		o, e := s.Orders.Create(ctx, order.CreateInput{UserID: owner.Session.UserID, AmountFen: 888, Channel: "alipay", Key: key, Message: "owner hidden message", IP: "192.0.2.1"})
		if e != nil {
			t.Fatal(e)
		}
		id, _ := storage.ParseID(o.ID)
		var paid, trade any
		if status == "paid" {
			paid = time.Now().Unix()
			trade = key
		}
		if _, e = s.Orders.Store.DB.ExecContext(ctx, "UPDATE orders SET status=?,paid_at=?,trade_no=?,hidden_order=?,hidden_message=1 WHERE id=?", status, paid, trade, hidden, id); e != nil {
			t.Fatal(e)
		}
		return o
	}
	visible := makeOrder("visible", "paid", false)
	makeOrder("pending", "pending", false)
	makeOrder("hidden", "paid", true)
	oid, _ := storage.ParseID(visible.ID)
	lid := storage.NewID()
	if _, e := s.Orders.Store.DB.ExecContext(ctx, "INSERT INTO order_locations(id,order_id,country,province,city,street,source,queried_at) VALUES(?,?,'中国','上海','合成测试城市','不公开街道','fixture-synthetic',?)", lid[:], oid, time.Now().Unix()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Queue.EnqueueOnce(ctx, visible.ID, administrator.Session.UserID, "manual-request", "reconcile"); e != nil {
		t.Fatal(e)
	}
	get := func(path, token string) (int, string, map[string]any) {
		t.Helper()
		request := httptest.NewRequest("GET", path, nil)
		if token != "" {
			request.AddCookie(&http.Cookie{Name: s.sessionName(), Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request)
		var body struct {
			Code string
			Data map[string]any
		}
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatal(e)
		}
		return w.Code, body.Code, body.Data
	}
	for _, tc := range []struct {
		name, token string
		mine        bool
	}{{"guest", "", false}, {"owner", owner.Token, true}, {"other", other.Token, false}, {"invalid-session", "invalid-cookie", false}} {
		t.Run(tc.name, func(t *testing.T) {
			status, code, data := get("/api/v1/order/list?user_id="+owner.Session.UserID, tc.token)
			if status != 200 || code != "OK" {
				t.Fatal(status, code)
			}
			items := data["items"].([]any)
			if len(items) != 1 {
				t.Fatalf("public visibility=%+v", items)
			}
			item := items[0].(map[string]any)
			if item["id"] != visible.ID || item["is_mine"] != tc.mine || item["status"] != "paid" || item["province"] != "上海" {
				t.Errorf("record=%+v", item)
			}
			if (item["message"] != "") != tc.mine {
				t.Errorf("hidden message=%+v", item)
			}
			if item["user_id"] != nil || item["ip"] != nil || item["location"] != nil {
				t.Errorf("private metadata=%+v", item)
			}
		})
	}
	for _, token := range []string{owner.Token, administrator.Token} {
		status, code, data := get("/api/v1/order/detail?id="+visible.ID, token)
		if status != 200 || code != "OK" || data["province"] != "上海" {
			t.Fatal(status, code, data)
		}
		for _, field := range []string{"ip", "location", "jobs", "reconciliations"} {
			if data[field] != nil {
				t.Errorf("public detail exposes %s: %+v", field, data)
			}
		}
	}
	status, code, _ := get("/api/v1/order/detail?id="+visible.ID, other.Token)
	if status != 403 || code != "FORBIDDEN" {
		t.Fatalf("other owner detail=%d %s", status, code)
	}
	status, code, _ = get("/api/v1/order/detail?id="+visible.ID, "")
	if status != 401 || code != "UNAUTHORIZED" {
		t.Fatalf("guest detail=%d %s", status, code)
	}
	status, code, data := get("/api/v1/admin/order/detail?id="+visible.ID, administrator.Token)
	if status != 200 || code != "OK" || data["location"] == nil || data["jobs"] == nil || len(data["reconciliations"].([]any)) != 1 {
		t.Fatalf("admin detail=%d %s %+v", status, code, data)
	}
}
