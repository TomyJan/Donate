package payment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
)

func TestGatewayFailureIdentifiesCauseWithoutLeakingResponse(t *testing.T) {
	for _, tc := range []struct {
		name, response, want string
		status               int
		base                 error
	}{
		{"signature rejected", `{"code":-3,"msg":"RSA签名校验失败 secret-value https://pay.test/?key=private"}`, "reason=request_signature", 200, ErrUnavailable},
		{"clock rejected", `{"code":-3,"msg":"时间戳字段不正确，请检查服务器时间"}`, "reason=request_timestamp", 200, ErrUnavailable},
		{"unsupported API", `{"code":-5,"msg":"接口方法不存在"}`, "reason=api_not_found", 200, ErrUnavailable},
		{"unknown upstream error", `{"code":-1,"msg":"secret-value https://pay.test/?key=private"}`, "code=-1", 200, ErrUnavailable},
		{"http failure", "secret-value", "http_status=503", 503, ErrUnavailable},
		{"html response", "<html>secret-value</html>", "stage=response_json", 200, ErrProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.response)
			}))
			defer server.Close()
			g, err := NewGateway(config.Config{EpayURL: server.URL, EpayPID: "1001", EpayKey: "secret-value"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = g.call(context.Background(), "api.php", "v1", map[string]string{"act": "order"})
			if !errors.Is(err, tc.base) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "api.php") {
				t.Fatalf("missing diagnostic: %v", err)
			}
			if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "https://") {
				t.Fatal("response contents leaked")
			}
		})
	}
}

type failingTransport func(*http.Request) (*http.Response, error)

func (f failingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGatewayTransportDiagnosticDoesNotExposeV1Key(t *testing.T) {
	g, err := NewGateway(config.Config{EpayURL: "https://gateway.test", EpayPID: "1001", EpayKey: "secret-value"})
	if err != nil {
		t.Fatal(err)
	}
	g.HTTP.Transport = failingTransport(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("request %s failed: %w", r.URL.String(), context.DeadlineExceeded)
	})
	_, err = g.call(context.Background(), "api.php", "v1", map[string]string{"act": "order"})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "reason=timeout") {
		t.Fatalf("missing transport diagnostic: %v", err)
	}
	if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "https://") {
		t.Fatal("request URL leaked")
	}
}
func TestNotificationDiagnosticSeparatesSignatureAndFields(t *testing.T) {
	g := &Gateway{Config: config.Config{EpayURL: "https://gateway.test", EpayPID: "1001", EpayKey: "private-key"}}
	o := order.Order{Protocol: "v1", GatewayID: "https://gateway.test", MerchantID: "1001", MerchantOrderNo: "merchant-one"}
	for _, tc := range []struct{ name, stage string }{{"signature", "signature"}, {"fields", "observation"}} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"pid": "1001", "out_trade_no": "merchant-one", "trade_no": "trade-one", "type": "alipay", "money": "0.01", "trade_status": "TRADE_SUCCESS", "private": "sensitive-value"}
			if tc.name == "fields" {
				values["money"] = "sensitive-value"
			}
			if err := g.sign(values, "v1"); err != nil {
				t.Fatal(err)
			}
			if tc.name == "signature" {
				values["sign"] = "sensitive-value"
			}
			_, err := g.Notification(o, values)
			if !errors.Is(err, ErrProtocol) || !strings.Contains(Diagnostic(err), "stage="+tc.stage) {
				t.Fatalf("missing notification stage: %v", err)
			}
			for _, secret := range []string{"private-key", "sensitive-value", "https://gateway.test"} {
				if strings.Contains(Diagnostic(err), secret) {
					t.Fatal("notification diagnostic leaked secret")
				}
			}
		})
	}
}
