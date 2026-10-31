package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestConcurrentStartReloadsSubmissionAfterAcquiringLease(t *testing.T) {
	for _, completed := range []bool{true, false} {
		name := "unknown_submission_queries_before_retry"
		if completed {
			name = "completed_checkout_is_reused"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "payment.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			uid := storage.NewID()
			if _, err := db.DB.Exec("INSERT INTO users(id,created_at) VALUES(?,?)", uid[:], time.Now().Unix()); err != nil {
				t.Fatal(err)
			}
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			var gateway *Gateway
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Query().Get("s"))
				if !completed || len(paths) > 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				out := map[string]string{"code": "0", "trade_no": "trade-existing", "pay_type": "jump", "pay_info": "https://gateway.test/pay/existing"}
				if err := gateway.sign(out, "v2"); err != nil {
					t.Error(err)
				}
				if err := json.NewEncoder(w).Encode(out); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			gateway = &Gateway{Config: config.Config{EpayURL: server.URL, EpayPID: "1001", PaymentRedirectHosts: []string{"gateway.test"}}, HTTP: server.Client(), Now: time.Now, private: key, public: &key.PublicKey}
			orders := order.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)), server.URL, "1001", "v2")
			o, err := orders.Create(ctx, order.CreateInput{UserID: uid.String(), AmountFen: 1, Channel: "alipay", Key: name})
			if err != nil {
				t.Fatal(err)
			}
			snapshotReady, resume := make(chan struct{}), make(chan struct{})
			var nowCalls atomic.Int32
			orders.Now = func() time.Time {
				if nowCalls.Add(1) == 1 {
					close(snapshotReady)
					<-resume
				}
				return time.Now()
			}
			service := &Service{Orders: orders, Gateway: gateway}
			type result struct {
				checkout Checkout
				err      error
			}
			delayed := make(chan result, 1)
			go func() { checkout, err := service.Start(ctx, o.ID, uid.String()); delayed <- result{checkout, err} }()
			<-snapshotReady
			first, firstErr := service.Start(ctx, o.ID, uid.String())
			close(resume)
			second := <-delayed
			wantPaths, wantStatus := "pay/create,pay/query", "unknown"
			if completed {
				wantPaths, wantStatus = "pay/create", "submitted"
				if firstErr != nil || second.err != nil || first.URL != "https://gateway.test/pay/existing" || second.checkout.URL != first.URL {
					t.Errorf("checkout not reused: first=%+v/%v second=%+v/%v", first, firstErr, second.checkout, second.err)
				}
			} else if !errors.Is(firstErr, ErrUnavailable) || !errors.Is(second.err, ErrUnavailable) {
				t.Errorf("unknown result must await successful recovery: first=%v second=%v", firstErr, second.err)
			}
			if strings.Join(paths, ",") != wantPaths {
				t.Errorf("gateway operations=%v, want %s", paths, wantPaths)
			}
			saved, err := orders.Get(ctx, o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.MerchantOrderNo != o.MerchantOrderNo || saved.Status != "pending" || saved.SubmissionStatus != wantStatus {
				t.Errorf("order changed unexpectedly: %+v", saved)
			}
			var lease sql.NullString
			bid, _ := storage.ParseID(o.ID)
			if err := db.DB.QueryRow("SELECT submission_lease FROM orders WHERE id=?", bid).Scan(&lease); err != nil {
				t.Fatal(err)
			}
			if lease.Valid {
				t.Error("submission lease was not released")
			}
		})
	}
}

func TestStartReleasesLeaseIfOrderBecomesUnpayable(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"paid", "UPDATE orders SET status='paid',paid_at=1,trade_no='trade-paid' WHERE id=?"},
		{"closed", "UPDATE orders SET status='closed' WHERE id=?"},
		{"expired", "UPDATE orders SET expires_at=1 WHERE id=?"},
		{"banned", "UPDATE users SET status='banned' WHERE id=(SELECT user_id FROM orders WHERE id=?)"},
		{"different_owner", "UPDATE orders SET user_id=(SELECT id FROM users WHERE id<>orders.user_id LIMIT 1) WHERE id=?"},
		{"closing", "UPDATE jobs SET kind='close' WHERE order_id=? AND kind='payment'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "payment.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			uid, other := storage.NewID(), storage.NewID()
			for _, id := range [][]byte{uid[:], other[:]} {
				if _, err := db.DB.Exec("INSERT INTO users(id,created_at) VALUES(?,?)", id, time.Now().Unix()); err != nil {
					t.Fatal(err)
				}
			}
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(503) }))
			defer server.Close()
			gateway := &Gateway{Config: config.Config{EpayURL: server.URL, EpayPID: "1001"}, HTTP: server.Client(), Now: time.Now, private: key, public: &key.PublicKey}
			orders := order.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)), server.URL, "1001", "v2")
			o, err := orders.Create(ctx, order.CreateInput{UserID: uid.String(), AmountFen: 1, Channel: "alipay", Key: test.name})
			if err != nil {
				t.Fatal(err)
			}
			bid, _ := storage.ParseID(o.ID)
			nowCalls := 0
			orders.Now = func() time.Time {
				nowCalls++
				if nowCalls == 4 {
					var lease sql.NullString
					if err := db.DB.QueryRow("SELECT submission_lease FROM orders WHERE id=?", bid).Scan(&lease); err != nil || !lease.Valid {
						t.Fatalf("expected acquired lease: %v", err)
					}
					if _, err := db.DB.Exec(test.mutation, bid); err != nil {
						t.Fatal(err)
					}
				}
				return time.Now()
			}
			if _, err := (&Service{Orders: orders, Gateway: gateway}).Start(ctx, o.ID, uid.String()); !errors.Is(err, order.ErrNotPayable) {
				t.Fatalf("unpayable start returned %v", err)
			}
			if calls.Load() != 0 {
				t.Fatal("unpayable order reached the gateway")
			}
			var lease sql.NullString
			if err := db.DB.QueryRow("SELECT submission_lease FROM orders WHERE id=?", bid).Scan(&lease); err != nil {
				t.Fatal(err)
			}
			if lease.Valid {
				t.Error("rejected start retained its lease")
			}
		})
	}
}
