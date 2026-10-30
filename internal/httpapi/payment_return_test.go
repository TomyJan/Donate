package httpapi

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestBrowserReturnIdentifiesOrderButDoesNotTrustSuccessParameter(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	login, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "return-owner"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.Orders.Create(ctx, order.CreateInput{UserID: login.Session.UserID, AmountFen: 888, Channel: "alipay", Key: "return-order", IP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{created.MerchantOrderNo, "missing"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/payment/return?"+url.Values{"out_trade_no": {number}, "trade_status": {"TRADE_SUCCESS"}}.Encode(), nil))
		target, err := url.Parse(w.Header().Get("Location"))
		if err != nil || w.Code != 303 || target.Path != "/" || target.Query().Get("payment") != "returned" {
			t.Fatalf("unexpected return: %d %s %v", w.Code, target, err)
		}
		want := ""
		if number == created.MerchantOrderNo {
			want = created.ID
		}
		if target.Query().Get("order") != want {
			t.Fatalf("order locator: %s", target)
		}
	}
	got, err := s.Orders.Get(ctx, created.ID)
	if err != nil || got.Status != "pending" {
		t.Fatalf("unverified browser return changed payment: %+v %v", got, err)
	}
	orderBytes, err := storage.ParseID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.Orders.Store.DB.QueryRow("SELECT count(*) FROM jobs WHERE order_id=? AND kind='payment'", orderBytes).Scan(&count); err != nil || count != 1 {
		t.Fatalf("missing verification job: %d %v", count, err)
	}
}
