package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
)

func TestV2CreateUsesPHPEntryAndIndependentSignatures(t *testing.T) {
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var mode string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api.php" || r.URL.Query().Get("s") != "pay/create" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		text := "clientip=192.0.2.1&method=jump&money=20.26&name=捐赠 TomyJan&notify_url=https://donate.test/api/v1/payment/notify&out_trade_no=merchant-one&pid=1001&return_url=https://donate.test/api/v1/payment/return&timestamp=1793318400&type=alipay"
		digest := sha256.Sum256([]byte(text))
		sig, err := base64.StdEncoding.DecodeString(r.PostForm.Get("sign"))
		if err != nil || rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, digest[:], sig) != nil {
			t.Error("merchant signature invalid")
		}
		if r.PostForm.Get("sign_type") != "RSA" {
			t.Error("wrong algorithm")
		}
		response := map[string]any{"code": 0, "trade_no": "gateway-one", "pay_type": "jump", "pay_info": "https://gateway.test/pay/submit/gateway-one/", "timestamp": "1793318400", "sign_type": "RSA"}
		if mode == "unapproved redirect" {
			response["pay_info"] = "https://other.test/pay"
		}
		if mode == "stale timestamp" {
			response["timestamp"] = "1793310000"
		}
		text = "code=0&pay_info=" + response["pay_info"].(string) + "&pay_type=jump&timestamp=" + response["timestamp"].(string) + "&trade_no=gateway-one"
		digest = sha256.Sum256([]byte(text))
		sig, err = rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if err != nil {
			t.Error(err)
			return
		}
		response["sign"] = base64.StdEncoding.EncodeToString(sig)
		if mode == "tampered response" {
			response["trade_no"] = "tampered"
		}
		if mode == "unsigned response" {
			delete(response, "sign")
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	g := &Gateway{Config: config.Config{EpayURL: server.URL, EpayPID: "1001", PublicURL: "https://donate.test", PaymentRedirectHosts: []string{"gateway.test"}}, HTTP: server.Client(), Now: func() time.Time { return time.Unix(1793318400, 0) }, private: merchant, public: &platform.PublicKey}
	o := order.Order{MerchantOrderNo: "merchant-one", Protocol: "v2", GatewayID: server.URL, MerchantID: "1001", Channel: "alipay", AmountFen: 2026, IP: "192.0.2.1"}
	checkout, trade, err := g.Create(context.Background(), o)
	if err != nil || checkout.Mode != "redirect" || checkout.URL != "https://gateway.test/pay/submit/gateway-one/" || trade != "gateway-one" {
		t.Fatalf("create failed: %+v, %s, %v", checkout, trade, err)
	}
	for _, tc := range []struct{ mode, reason string }{
		{"tampered response", "response_signature"},
		{"unsigned response", "response_signature"},
		{"stale timestamp", "response_timestamp"},
		{"unapproved redirect", "checkout_redirect_not_allowed"},
	} {
		mode = tc.mode
		if _, _, err := g.Create(context.Background(), o); !errors.Is(err, ErrProtocol) || !strings.Contains(Diagnostic(err), tc.reason) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
}
