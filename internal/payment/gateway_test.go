package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
)

func TestGatewayPreservesRetryAfterWithoutTrustingPaymentData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
		w.Write([]byte(`{"status":1}`))
	}))
	defer server.Close()
	g, e := NewGateway(config.Config{Development: true, EpayURL: server.URL, EpayPID: "1001", EpayKey: "secret", EpayProtocol: "v1"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = g.Query(context.Background(), order.Order{Protocol: "v1", GatewayID: server.URL, MerchantID: "1001"})
	var retry interface{ RetryAfter() time.Duration }
	if !errors.Is(e, ErrUnavailable) || !errors.As(e, &retry) || retry.RetryAfter() != 120*time.Second {
		t.Fatalf("rate limit lost: %v", e)
	}
}

func TestMD5KeepsZeroAndRejectsDuplicateParameters(t *testing.T) {
	if got := canonical(map[string]string{"a": "0", "b": "", "sign": "ignored", "sign_type": "MD5"}); got != "a=0" {
		t.Fatal(got)
	}
	if _, e := ParseValues(url.Values{"pid": {"1001", "9999"}, "money": {"1.00"}}); e == nil {
		t.Fatal("duplicate accepted")
	}
}
func TestV2QueryVerifiesPlatformAndPaymentState(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	priv, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	tamper := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.php" || r.URL.Query().Get("s") != "pay/query" || r.Method != "POST" {
			t.Error("wrong API")
		}
		r.ParseForm()
		if r.PostForm.Get("out_trade_no") != "merchant-one" {
			t.Error("wrong order")
		}
		sig, e := base64.StdEncoding.DecodeString(r.PostForm.Get("sign"))
		if e != nil {
			t.Error(e)
		}
		v := map[string]string{}
		for k, x := range r.PostForm {
			if len(x) == 1 {
				v[k] = x[0]
			}
		}
		h := sha256.Sum256([]byte(canonical(v)))
		if e = rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, h[:], sig); e != nil {
			t.Error("request signature", e)
		}
		response := map[string]string{"code": "0", "pid": "1001", "out_trade_no": "merchant-one", "trade_no": "gateway-one", "type": "alipay", "money": "8.88", "status": "1", "timestamp": "1793318400", "sign_type": "RSA"}
		// Independently constructed fixture catches sorting, zero filtering and signature verification errors.
		text := "code=0&money=8.88&out_trade_no=merchant-one&pid=1001&status=1&timestamp=1793318400&trade_no=gateway-one&type=alipay"
		digest := sha256.Sum256([]byte(text))
		signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if e != nil {
			t.Error(e)
		}
		response["sign"] = base64.StdEncoding.EncodeToString(signature)
		if tamper {
			response["money"] = "88.88"
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	g, e := NewGateway(config.Config{Development: true, EpayURL: server.URL, EpayPID: "1001", EpayProtocol: "v2", EpayPrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv})), EpayPublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))})
	if e != nil {
		t.Fatal(e)
	}
	g.Now = func() time.Time { return time.Unix(1793318400, 0) }
	o := order.Order{MerchantOrderNo: "merchant-one", Protocol: "v2", GatewayID: server.URL, MerchantID: "1001"}
	result, e := g.Query(context.Background(), o)
	if e != nil || !result.Verified || result.State != "paid" || result.AmountFen != 888 {
		t.Fatal(result, e)
	}
	tamper = true
	if _, e = g.Query(context.Background(), o); e == nil {
		t.Fatal("tampered response accepted")
	}
}
