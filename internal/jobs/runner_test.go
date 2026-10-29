package jobs

import (
	"context"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

type fakeGateway struct {
	queries int
	closes  int
}

func (g *fakeGateway) Ready() bool { return true }
func (g *fakeGateway) Query(_ context.Context, o order.Order) (order.Observation, error) {
	g.queries++
	return order.Observation{MerchantOrderNo: o.MerchantOrderNo, MerchantID: o.MerchantID, GatewayID: o.GatewayID, TradeNo: "runner-trade", AmountFen: o.AmountFen, Channel: o.Channel, State: "paid", Verified: true}, nil
}
func (g *fakeGateway) Close(context.Context, order.Order) error { g.closes++; return nil }
func TestRunnerAppliesAndEndsJobAtomically(t *testing.T) {
	ctx := context.Background()
	db, e := storage.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	now := time.Now()
	id := storage.NewID()
	_, e = db.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'runner',1,'alipay',?,?,'127.0.0.1')", id[:], now.Unix(), now.Add(time.Minute).Unix())
	if e != nil {
		t.Fatal(e)
	}
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := NewQueue(db, l)
	_, _ = q.Enqueue(ctx, "payment", id.String(), "manual", "", "req")
	j, e := q.Claim(ctx, []string{"payment"})
	if e != nil {
		t.Fatal(e)
	}
	g := &fakeGateway{}
	s := order.New(db, l, "", "", "v2")
	r := Runner{Queue: q, Orders: s, Gateway: g}
	r.Execute(ctx, j)
	o, e := s.Get(ctx, id.String())
	if e != nil || o.Status != "paid" {
		t.Fatal(e, o.Status)
	}
	job, e := q.Get(ctx, j.ID)
	if e != nil || job.Status != "done" {
		t.Fatal(e, job.Status)
	}
	if g.queries != 1 {
		t.Fatal(g.queries)
	}
}
func TestScheduleIsFiniteAndIncludesExpiry(t *testing.T) {
	created := time.Now()
	next, ok := NextPayment(created, created.Add(18*time.Minute))
	if !ok || !next.Equal(created.Add(30*time.Minute)) {
		t.Fatal(next)
	}
	if _, ok = NextPayment(created, created.Add(24*time.Hour)); ok {
		t.Fatal("poll window unbounded")
	}
}
