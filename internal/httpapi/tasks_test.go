package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestTaskRoutesRequireAdminAndCSRF(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	c, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "tasks", Subject: "ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"list", "detail", "history", "overview", "retry"} {
		for _, logged := range []bool{false, true} {
			method := "GET"
			if path == "retry" {
				method = "POST"
			}
			r := httptest.NewRequest(method, "/api/v1/admin/task/"+path, strings.NewReader(`{"ids":[],"idempotency_key":"one"}`))
			if logged {
				r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := 401
			if logged {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("%s logged=%v: %d %s", path, logged, w.Code, w.Body.String())
			}
		}
	}
	s.Config.AdminIDs[c.Session.UserID] = true
	r := httptest.NewRequest("POST", "/api/v1/admin/task/retry", strings.NewReader(`{"ids":[],"idempotency_key":"one"}`))
	r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "CSRF_FAILED") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestTaskRetryPersistsOriginalResponseAndDoesNotReplay(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	c, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "tasks", Subject: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	s.Config.AdminIDs[c.Session.UserID] = true
	now := time.Now().UTC().Truncate(time.Second)
	s.Admin.Now = func() time.Time { return now }
	s.Queue.Now = func() time.Time { return now }
	oid := storage.NewID()
	if _, err = s.Admin.Store.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'task-http',1,'alipay',?,?,'192.0.2.1')", oid[:], now.Unix(), now.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	jid, err := s.Queue.Enqueue(ctx, "close", oid.String(), "automatic", "", "task-http")
	if err != nil {
		t.Fatal(err)
	}
	bid, _ := storage.ParseID(jid)
	if _, err = s.Admin.Store.DB.Exec("UPDATE jobs SET status='failed',attempts=5,last_result='close_failed' WHERE id=?", bid); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) (string, json.RawMessage) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", s.Config.PublicURL)
		r.Header.Set("X-CSRF-Token", c.Session.CSRF)
		r.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var v struct {
			Code string          `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return v.Code, v.Data
	}
	body := fmt.Sprintf(`{"ids":[%q],"idempotency_key":"retry-once"}`, jid)
	code, first := request("POST", "/api/v1/admin/task/retry", body)
	if code != "OK" || !strings.Contains(string(first), `"queued":1`) {
		t.Fatal(code, string(first))
	}
	var status string
	var attempts int
	if err = s.Admin.Store.DB.QueryRow("SELECT status,attempts FROM jobs WHERE id=?", bid).Scan(&status, &attempts); err != nil || status != "ready" || attempts != 0 {
		t.Fatal(status, attempts, err)
	}
	if _, err = s.Queue.Claim(ctx, []string{"close"}); err != nil {
		t.Fatal(err)
	}
	code, second := request("POST", "/api/v1/admin/task/retry", body)
	if code != "OK" || string(first) != string(second) {
		t.Fatal(code, string(first), string(second))
	}
	if err = s.Admin.Store.DB.QueryRow("SELECT status,attempts FROM jobs WHERE id=?", bid).Scan(&status, &attempts); err != nil || status != "running" || attempts != 1 {
		t.Fatal(status, attempts, err)
	}
	code, _ = request("POST", "/api/v1/admin/task/retry", fmt.Sprintf(`{"ids":[%q],"idempotency_key":"retry-once"}`, storage.NewID().String()))
	if code != "IDEMPOTENCY_CONFLICT" {
		t.Fatal(code)
	}
	for _, path := range []string{"list?status=bogus", "list?page=0", "list?page_size=101", "list?kind=bogus", "list?sort=lease_token", "list?direction=oops", "list?from=bad", "overview?range=bad", "history?id=bad"} {
		code, _ = request("GET", "/api/v1/admin/task/"+path, "")
		if code != "VALIDATION_FAILED" {
			t.Fatal(path, code)
		}
	}
	code, data := request("GET", "/api/v1/admin/task/list?status=running&search=task-http", "")
	if code != "OK" || !strings.Contains(string(data), jid) || strings.Contains(string(data), "lease_token") {
		t.Fatal(code, string(data))
	}
	code, data = request("GET", "/api/v1/admin/task/detail?id="+jid, "")
	if code != "OK" || !strings.Contains(string(data), "recording_since") {
		t.Fatal(code, string(data))
	}
	code, data = request("GET", "/api/v1/admin/task/history?id="+jid+"&page_size=2", "")
	if code != "OK" || !strings.Contains(string(data), `"page_size":2`) {
		t.Fatal(code, string(data))
	}
	code, data = request("GET", "/api/v1/admin/task/overview", "")
	if code != "OK" || !strings.Contains(string(data), "trend") {
		t.Fatal(code, string(data))
	}
	var n int
	aid, _ := storage.ParseID(c.Session.UserID)
	if err = s.Admin.Store.DB.QueryRow("SELECT count(*) FROM audit_events WHERE actor_id=? AND action='task.retry' AND target_type='order' AND target_id=?", aid, oid[:]).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
