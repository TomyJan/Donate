package httpapi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
)

func TestSignedPaidNotificationBackfillsMissingChannelTradeNumber(t *testing.T) {
	s, h, a := setup(t)
	ctx := context.Background()
	s.Orders.GatewayID, s.Orders.MerchantID, s.Orders.Protocol = "https://gateway.test", "1001", "v1"
	s.Payments.Gateway.Config.EpayKey = "test-secret"
	creds, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "enrichment"})
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.Orders.Create(ctx, order.CreateInput{UserID: creds.Session.UserID, AmountFen: 1, Channel: "alipay", Key: "enrichment"})
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"money": {"0.01"}, "out_trade_no": {o.MerchantOrderNo}, "pid": {"1001"}, "trade_no": {"platform-one"}, "trade_status": {"TRADE_SUCCESS"}, "type": {"alipay"}}
	send := func(apiTradeNo string, success bool) {
		t.Helper()
		values.Del("api_trade_no")
		if apiTradeNo != "" {
			values.Set("api_trade_no", apiTradeNo)
		}
		keys := []string{}
		for k := range values {
			if k != "sign" && k != "sign_type" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		parts := []string{}
		for _, k := range keys {
			parts = append(parts, k+"="+values.Get(k))
		}
		sum := md5.Sum([]byte(strings.Join(parts, "&") + "test-secret"))
		values.Set("sign", hex.EncodeToString(sum[:]))
		values.Set("sign_type", "MD5")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/payment/notify?"+values.Encode(), nil))
		if (w.Code == 200 && w.Body.String() == "success") != success {
			t.Fatalf("notification result: %d %s", w.Code, w.Body.String())
		}
	}
	send("", true)
	first, err := s.Orders.Get(ctx, o.ID)
	if err != nil || first.PaidAt == nil {
		t.Fatalf("first payment: %+v %v", first, err)
	}
	s.Orders.Now = func() time.Time { return first.PaidAt.Add(time.Minute) }
	send("channel-one", true)
	got, err := s.Orders.Get(ctx, o.ID)
	if err != nil || got.APITradeNo != "channel-one" {
		t.Fatalf("missing trusted identifier was not backfilled: %q %v", got.APITradeNo, err)
	}
	send("channel-one", true)
	send("", true)
	send("channel-conflict", false)
	got, err = s.Orders.Get(ctx, o.ID)
	if err != nil || got.Status != "paid" || got.APITradeNo != "channel-one" || got.TradeNo != "platform-one" || got.PaidAt == nil || !got.PaidAt.Equal(*first.PaidAt) {
		t.Fatalf("payment identity/time changed: %+v %v", got, err)
	}
	for _, query := range []string{"SELECT count(*) FROM audit_events WHERE action='order.paid'", "SELECT count(*) FROM payment_observations WHERE status='paid'"} {
		var count int
		if err := s.Orders.Store.DB.QueryRow(query).Scan(&count); err != nil || count != 1 {
			t.Fatalf("paid side effects repeated: count=%d err=%v query=%s", count, err, query)
		}
	}
}
