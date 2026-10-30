package payment

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestSubmissionFailureLogsSafeCauseAndKeepsOriginalOrder(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "payment.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	uid := storage.NewID()
	if _, err := db.DB.Exec("INSERT INTO users(id,created_at) VALUES(?,?)", uid[:], time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Query().Get("s"))
		message, code := "RSA签名校验失败 sensitive-value", -3
		if r.URL.Query().Get("s") == "pay/query" {
			message, code = "订单号不存在 sensitive-value", -1
		}
		json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": message})
	}))
	defer server.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	g := &Gateway{Config: config.Config{EpayURL: server.URL, EpayPID: "1001"}, HTTP: server.Client(), Now: time.Now, private: key, public: &key.PublicKey}
	orders := order.New(db, logger, server.URL, "1001", "v2")
	o, err := orders.Create(ctx, order.CreateInput{UserID: uid.String(), AmountFen: 1, Channel: "alipay", Key: "original", IP: "192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	s := Service{Orders: orders, Gateway: g}
	for range 2 {
		if _, err = s.Start(ctx, o.ID, uid.String()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unexpected submission result: %v", err)
		}
	}
	if strings.Join(calls, ",") != "pay/create,pay/query" {
		t.Fatalf("uncertain create repeated: %v", calls)
	}
	got, err := orders.Get(ctx, o.ID)
	if err != nil || got.MerchantOrderNo != o.MerchantOrderNo || got.SubmissionStatus != "unknown" || got.Status != "pending" {
		t.Fatalf("uncertain order changed: %+v %v", got, err)
	}
	warnings := 0
	for _, line := range bytes.Split(logs.Bytes(), []byte{10}) {
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if entry["level"] != "WARN" {
			continue
		}
		warnings++
		if entry["order_id"] != o.ID || entry["request_id"] == "" || !strings.Contains(entry["msg"].(string), o.ID) || !strings.Contains(entry["msg"].(string), "reason=") || entry["error"] == nil {
			t.Fatalf("unactionable warning: %s", line)
		}
	}
	if warnings != 2 || strings.Contains(logs.String(), "sensitive-value") {
		t.Fatal("missing diagnostics or sensitive upstream text leaked")
	}
}
