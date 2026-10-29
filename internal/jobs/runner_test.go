package jobs

import (
	"context"
	"fmt"
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

type closeGateway struct {
	queries   int
	fail      bool
	queryFail bool
}

func (g *closeGateway) Ready() bool { return true }
func (g *closeGateway) Query(_ context.Context, o order.Order) (order.Observation, error) {
	g.queries++
	state := "unpaid"
	if g.queryFail {
		return order.Observation{}, context.DeadlineExceeded
	}
	if !g.fail && g.queries > 1 {
		state = "paid"
	}
	return order.Observation{MerchantOrderNo: o.MerchantOrderNo, MerchantID: o.MerchantID, GatewayID: o.GatewayID, TradeNo: "review-trade", AmountFen: o.AmountFen, Channel: o.Channel, State: state, Verified: true}, nil
}
func (g *closeGateway) Close(context.Context, order.Order) error {
	if g.fail {
		return context.DeadlineExceeded
	}
	return nil
}
func TestRegressionCloseMustVerifyAgainAfterACK(t *testing.T) {
	ctx := context.Background()
	db, e := storage.Open(ctx, filepath.Join(t.TempDir(), "review.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	now := time.Now()
	id := storage.NewID()
	_, e = db.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'review-close',1,'alipay',?,?,'127.0.0.1')", id[:], now.Unix(), now.Add(time.Hour).Unix())
	if e != nil {
		t.Fatal(e)
	}
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := NewQueue(db, l)
	q.Enqueue(ctx, "close", id.String(), "admin", "", "req")
	j, e := q.Claim(ctx, []string{"close"})
	if e != nil {
		t.Fatal(e)
	}
	g := &closeGateway{}
	s := order.New(db, l, "", "", "v2")
	r := Runner{Queue: q, Orders: s, Gateway: g}
	r.Execute(ctx, j)
	o, e := s.Get(ctx, id.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("queries=%d status=%s remote_close=%s", g.queries, o.Status, o.RemoteCloseStatus)
	if g.queries < 2 || o.Status != "paid" {
		t.Fatalf("close ACK must be followed by final payment verification")
	}
}
func TestRegressionFailedCloseMustKeepLocalEntranceClosed(t *testing.T) {
	for _, queryFail := range []bool{false, true} {
		t.Run(fmt.Sprintf("query_failure=%t", queryFail), func(t *testing.T) {
			testFailedClose(t, queryFail)
		})
	}
}
func testFailedClose(t *testing.T, queryFail bool) {
	ctx := context.Background()
	db, e := storage.Open(ctx, filepath.Join(t.TempDir(), "review.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	now := time.Now()
	id := storage.NewID()
	_, e = db.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'review-failed-close',1,'alipay',?,?,'127.0.0.1')", id[:], now.Unix(), now.Add(time.Hour).Unix())
	if e != nil {
		t.Fatal(e)
	}
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := NewQueue(db, l)
	q.Now = func() time.Time { return now }
	q.Enqueue(ctx, "close", id.String(), "admin", "", "req")
	g := &closeGateway{fail: true, queryFail: queryFail}
	s := order.New(db, l, "", "", "v2")
	s.Now = q.Now
	r := Runner{Queue: q, Orders: s, Gateway: g}
	var last Job
	for i := 0; i < 5; i++ {
		j, e := q.Claim(ctx, []string{"close"})
		if e != nil {
			t.Fatal(e)
		}
		r.next = time.Time{}
		r.blocked = time.Time{}
		r.Execute(ctx, j)
		last, e = q.Get(ctx, j.ID)
		if e != nil {
			t.Fatal(e)
		}
		now = last.RunAt.Add(time.Second)
	}
	o, e := s.Get(ctx, id.String())
	if e != nil {
		t.Fatal(e)
	}
	var active int
	db.DB.QueryRow("SELECT count(*) FROM jobs WHERE order_id=? AND kind='close' AND status IN ('ready','running')", id[:]).Scan(&active)
	t.Logf("status=%s close_job=%s active_close_jobs=%d attempts=%d", o.Status, last.Status, active, last.Attempts)
	if o.Status != "closed" {
		t.Fatal("local entrance reopens after remote close retries are exhausted")
	}
	if last.Status != "failed" || active != 0 || last.Attempts != 5 {
		t.Fatal("remote close retry budget was not exhausted", last)
	}
	var followups int
	if e := db.DB.QueryRow("SELECT count(*) FROM jobs WHERE order_id=? AND kind='payment' AND status='ready'", id[:]).Scan(&followups); e != nil || followups != 1 {
		t.Fatal("missing durable payment followup", followups, e)
	}
}
