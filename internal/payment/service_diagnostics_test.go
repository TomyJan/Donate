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

func TestSubmissionReleaseFailureLogsOnceAndPreservesGatewayError(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "payment.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	uid := storage.NewID()
	if _, err = db.DB.Exec("INSERT INTO users(id,created_at) VALUES(?,?)", uid[:], time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	orders := order.New(db, logger, server.URL, "1001", "v2")
	o, err := orders.Create(ctx, order.CreateInput{UserID: uid.String(), AmountFen: 1, Channel: "alipay", Key: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("CREATE TRIGGER reject_release BEFORE UPDATE OF submission_lease ON orders WHEN NEW.submission_lease IS NULL AND OLD.submission_lease IS NOT NULL BEGIN SELECT RAISE(ABORT,'private-release-detail'); END"); err != nil {
		t.Fatal(err)
	}
	gateway := &Gateway{Config: config.Config{EpayURL: server.URL, EpayPID: "1001"}, HTTP: server.Client(), Now: time.Now, private: key, public: &key.PublicKey}
	if _, err = (&Service{Orders: orders, Gateway: gateway}).Start(ctx, o.ID, uid.String()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	failures := 0
	for _, line := range bytes.Split(logs.Bytes(), []byte{10}) {
		var entry map[string]any
		if json.Unmarshal(line, &entry) != nil || entry["stage"] != "submission_release" {
			continue
		}
		failures++
		if entry["order_id"] != o.ID || entry["request_id"] == "" || entry["reason"] != "sqlite_1811" {
			t.Fatal(entry)
		}
	}
	if failures != 1 || strings.Contains(logs.String(), "private-release-detail") {
		t.Fatalf("duplicate or unsafe release diagnostics: %s", logs.String())
	}
}

func TestCheckoutPersistenceFailureLogsSafePhaseAndOrder(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "payment.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	uid := storage.NewID()
	if _, err = db.DB.Exec("INSERT INTO users(id,created_at) VALUES(?,?)", uid[:], time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	orders := order.New(db, logger, "https://gateway.test", "1001", "v1")
	o, err := orders.Create(ctx, order.CreateInput{UserID: uid.String(), AmountFen: 1, Channel: "alipay", Key: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("CREATE TRIGGER reject_checkout BEFORE UPDATE OF submission_status ON orders BEGIN SELECT RAISE(ABORT,'private-checkout-storage-error'); END"); err != nil {
		t.Fatal(err)
	}
	gateway := &Gateway{Config: config.Config{EpayURL: "https://gateway.test", EpayPID: "1001", EpayKey: "private-key"}}
	_, err = (&Service{Orders: orders, Gateway: gateway}).Start(ctx, o.ID, uid.String())
	if err == nil || !strings.Contains(err.Error(), "private-checkout-storage-error") {
		t.Fatal(err)
	}
	var failure map[string]any
	for _, line := range bytes.Split(logs.Bytes(), []byte{10}) {
		var entry map[string]any
		if json.Unmarshal(line, &entry) == nil && entry["stage"] == "checkout_persist" {
			failure = entry
		}
	}
	if failure["order_id"] != o.ID || failure["user_id"] != uid.String() || failure["reason"] != "sqlite_1811" || failure["duration_ms"] == nil || !strings.Contains(failure["msg"].(string), o.ID) {
		t.Fatalf("missing useful payment failure: %s", logs.String())
	}
	for _, secret := range []string{"private-key", "private-checkout-storage-error", "https://gateway.test", "sign="} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("payment log exposed %q", secret)
		}
	}
}
