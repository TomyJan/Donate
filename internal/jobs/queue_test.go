package jobs

import (
	"context"
	"errors"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestManualReconcileRejectsInvalidIdempotencyKey(t *testing.T) {
	q := &Queue{}
	if _, e := q.EnqueueOnce(context.Background(), "", "", "", ""); !errors.Is(e, order.ErrInvalid) {
		t.Fatalf("not a business validation error: %v", e)
	}
}

func TestLeaseRecoveryRejectsOldWorker(t *testing.T) {
	db, e := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	now := time.Now().Truncate(time.Second)
	id := storage.NewID()
	_, e = db.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?, 'one',1,'alipay',?,?,'192.0.2.1')", id[:], now.Unix(), now.Add(time.Minute).Unix())
	if e != nil {
		t.Fatal(e)
	}
	q := NewQueue(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	q.Now = func() time.Time { return now }
	if _, e = q.Enqueue(context.Background(), "payment", id.String(), "automatic", "", ""); e != nil {
		t.Fatal(e)
	}
	first, e := q.Claim(context.Background(), []string{"payment"})
	if e != nil {
		t.Fatal(e)
	}
	now = now.Add(61 * time.Second)
	second, e := q.Claim(context.Background(), []string{"payment"})
	if e != nil {
		t.Fatal(e)
	}
	if first.ID != second.ID || first.LeaseToken == second.LeaseToken {
		t.Fatal("lease did not recover")
	}
	if ok, e := q.Finish(context.Background(), first, "done", "old", time.Time{}); e != nil || ok {
		t.Fatal("stale worker wrote", e)
	}
	if ok, e := q.Finish(context.Background(), second, "done", "new", time.Time{}); e != nil || !ok {
		t.Fatal(e)
	}
}
