package httpapi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"github.com/TomyJan/Donate/internal/admin"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/jobs"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/payment"
	"github.com/TomyJan/Donate/internal/storage"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPaymentNotificationAcknowledgesOnlyCommittedPayment(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	s.Orders.GatewayID = "https://gateway.test"
	s.Orders.MerchantID = "1001"
	s.Orders.Protocol = "v1"
	s.Payments.Gateway.Config.EpayKey = "test-secret"
	c, e := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "notify"})
	if e != nil {
		t.Fatal(e)
	}
	o, e := s.Orders.Create(ctx, order.CreateInput{UserID: c.Session.UserID, AmountFen: 1, Channel: "alipay", Key: "notify", IP: "127.0.0.1"})
	if e != nil {
		t.Fatal(e)
	}
	values := url.Values{"money": {"0.01"}, "out_trade_no": {o.MerchantOrderNo}, "pid": {"1001"}, "trade_no": {"notify-one"}, "trade_status": {"TRADE_SUCCESS"}, "type": {"alipay"}}
	canonical := "money=0.01&out_trade_no=" + o.MerchantOrderNo + "&pid=1001&trade_no=notify-one&trade_status=TRADE_SUCCESS&type=alipaytest-secret"
	sum := md5.Sum([]byte(canonical))
	values.Set("sign", hex.EncodeToString(sum[:]))
	values.Set("sign_type", "MD5")
	if _, e = s.Orders.Store.DB.Exec("CREATE TRIGGER reject_payment BEFORE INSERT ON audit_events WHEN NEW.action='order.paid' BEGIN SELECT RAISE(ABORT,'simulated commit failure'); END"); e != nil {
		t.Fatal(e)
	}
	send := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/payment/notify?"+values.Encode(), nil))
		return w
	}
	failed := send()
	if failed.Code != 500 || failed.Body.String() != "fail" {
		t.Fatal(failed.Code, failed.Body.String())
	}
	got, e := s.Orders.Get(ctx, o.ID)
	if e != nil || got.Status != "pending" {
		t.Fatal(got.Status, e)
	}
	if _, e = s.Orders.Store.DB.Exec("DROP TRIGGER reject_payment"); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		w := send()
		if w.Code != 200 || w.Body.String() != "success" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	got, e = s.Orders.Get(ctx, o.ID)
	if e != nil || got.Status != "paid" {
		t.Fatal(got.Status, e)
	}
}

func TestOpenAPIIncludesEveryAPIAndHealthRoute(t *testing.T) {
	_, h, _ := setup(t)
	data, e := os.ReadFile("../../api/openapi.json")
	if e != nil {
		t.Fatal(e)
	}
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if e = json.Unmarshal(data, &spec); e != nil {
		t.Fatal(e)
	}
	e = chi.Walk(h.(chi.Routes), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if path == "/*" {
			return nil
		}
		if len(spec.Paths[path][strings.ToLower(method)]) == 0 {
			t.Errorf("missing contract: %s %s", method, path)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}

func setup(t *testing.T) (*Server, http.Handler, *auth.Service) {
	t.Helper()
	db, e := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{Development: true, PublicURL: "http://localhost", Secret: make([]byte, 32), AdminIDs: map[string]bool{}}
	a := auth.New(db, cfg, l)
	o := order.New(db, l, "g", "p", "v2")
	g, _ := payment.NewGateway(cfg)
	g.Config.EpayURL = "https://gateway.test"
	g.Config.EpayPID = "1001"
	s := &Server{Config: cfg, Auth: a, Orders: o, Payments: &payment.Service{Orders: o, Gateway: g}, Admin: admin.New(db, o, cfg, l), Queue: jobs.NewQueue(db, l), Log: l, Assets: fstest.MapFS{"index.html": {Data: []byte("app")}}}
	return s, s.Handler(), a
}
func TestAuthenticationBusinessCodesAndStaticBoundary(t *testing.T) {
	s, h, a := setup(t)
	c, e := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "test", Subject: "one"})
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		method, path, body string
		login, csrf        bool
		status             int
		code               string
	}{{"GET", "/api/v1/user/profile", "", false, false, 401, "UNAUTHORIZED"}, {"GET", "/api/v1/admin/order/list", "", true, false, 403, "FORBIDDEN"}, {"POST", "/api/v1/order/create", `{"amount_fen":0,"channel":"alipay","idempotency_key":"one"}`, true, true, 200, "VALIDATION_FAILED"}, {"POST", "/api/v1/order/create", `{}`, true, false, 403, "CSRF_FAILED"}}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Origin", s.Config.PublicURL)
		if tc.login {
			req.AddCookie(&http.Cookie{Name: s.sessionName(), Value: c.Token})
		}
		if tc.csrf {
			req.Header.Set("X-CSRF-Token", c.Session.CSRF)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		var b struct {
			Code string `json:"code"`
		}
		json.Unmarshal(w.Body.Bytes(), &b)
		if w.Code != tc.status || b.Code != tc.code {
			t.Fatalf("%s %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/missing", "/.env", "/data/donate.db"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatal("static fallback leaked", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Body.String() != "app" {
		t.Fatal("home route", w.Code, w.Body.String())
	}
}
func TestClientIPUsesOnlyConfiguredHeader(t *testing.T) {
	for _, tc := range []struct {
		name, header, value, remote, want string
	}{
		{"unconfigured", "", "", "198.51.100.2:1234", "198.51.100.2"},
		{"custom header", "X-Client-IP", "203.0.113.7", "198.51.100.2:1234", "203.0.113.7"},
		{"real ip", "X-Real-IP", "203.0.113.8", "198.51.100.2:1234", "203.0.113.8"},
		{"ipv6", "X-Real-IP", "2001:db8::7", "198.51.100.2:1234", "2001:db8::7"},
		{"mapped ipv4", "X-Real-IP", "::ffff:203.0.113.7", "198.51.100.2:1234", "203.0.113.7"},
		{"forwarded chain", "X-Forwarded-For", "203.0.113.7, 192.0.2.3", "198.51.100.2:1234", "203.0.113.7"},
		{"spaces", "X-Real-IP", " 203.0.113.7 ", "198.51.100.2:1234", "203.0.113.7"},
		{"missing", "X-Client-IP", "", "198.51.100.2:1234", "198.51.100.2"},
		{"invalid", "X-Real-IP", "not-an-ip", "198.51.100.2:1234", "198.51.100.2"},
		{"empty first hop", "X-Forwarded-For", ", 203.0.113.7", "198.51.100.2:1234", "198.51.100.2"},
		{"invalid chain", "X-Forwarded-For", "203.0.113.7, unknown", "198.51.100.2:1234", "198.51.100.2"},
		{"overlong chain", "X-Forwarded-For", strings.Repeat("203.0.113.7,", 32) + "192.0.2.3", "198.51.100.2:1234", "198.51.100.2"},
		{"zone", "X-Real-IP", "fe80::1%eth0", "198.51.100.2:1234", "198.51.100.2"},
		{"port", "X-Real-IP", "203.0.113.7:1234", "198.51.100.2:1234", "198.51.100.2"},
		{"remote ipv6", "", "", "[2001:db8::2]:1234", "2001:db8::2"},
		{"bare remote", "", "", "198.51.100.2", "198.51.100.2"},
		{"invalid remote", "", "", "invalid", "0.0.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Config: config.Config{RealIPHeader: tc.header}}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", "1.1.1.1")
			r.Header.Set("X-Real-IP", "1.1.1.2")
			if tc.header != "" {
				r.Header.Set(tc.header, tc.value)
			}
			if got := s.clientIP(r); got != tc.want {
				t.Fatalf("IP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientIPRejectsDuplicateHeaders(t *testing.T) {
	s := &Server{Config: config.Config{RealIPHeader: "X-Real-IP"}}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "198.51.100.2:1234"
	r.Header.Add("X-Real-IP", "203.0.113.7")
	r.Header.Add("X-Real-IP", "203.0.113.8")
	if got := s.clientIP(r); got != "198.51.100.2" {
		t.Fatalf("ambiguous header accepted: %q", got)
	}
}
