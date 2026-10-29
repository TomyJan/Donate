package jobs

import (
	"context"
	"database/sql"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

type closeRaceGateway struct {
	query  func(order.Order) (order.Observation, error)
	closes int
}

func (g *closeRaceGateway) Ready() bool { return true }
func (g *closeRaceGateway) Query(_ context.Context, o order.Order) (order.Observation, error) {
	return g.query(o)
}
func (g *closeRaceGateway) Close(context.Context, order.Order) error { g.closes++; return nil }
func closeRaceSetup(t *testing.T) (*storage.Store, *order.Service, *Queue, *time.Time, string) {
	t.Helper()
	ctx := context.Background()
	db, e := storage.Open(ctx, filepath.Join(t.TempDir(), "closeRace.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Now().Truncate(time.Second)
	id := storage.NewID()
	_, e = db.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'closeRace',1,'alipay',?,?,'127.0.0.1')", id[:], now.Unix(), now.Add(30*time.Minute).Unix())
	if e != nil {
		t.Fatal(e)
	}
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := NewQueue(db, l)
	q.Now = func() time.Time { return now }
	s := order.New(db, l, "", "", "v2")
	s.Now = q.Now
	if _, e = q.Enqueue(ctx, "close", id.String(), "admin", "", "req"); e != nil {
		t.Fatal(e)
	}
	return db, s, q, &now, id.String()
}
func closeRaceObservation(o order.Order, state string) order.Observation {
	return order.Observation{MerchantOrderNo: o.MerchantOrderNo, MerchantID: o.MerchantID, GatewayID: o.GatewayID, TradeNo: "closeRace-trade", AmountFen: o.AmountFen, Channel: o.Channel, State: state, Verified: true}
}
func TestCloseLocalStateAndPaymentFollowupCommitTogether(t *testing.T) {
	db, s, q, _, id := closeRaceSetup(t)
	ctx := context.Background()
	j, e := q.Claim(ctx, []string{"close"})
	if e != nil {
		t.Fatal(e)
	}
	o, e := s.Get(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	r := Runner{Queue: q, Orders: s}
	if e = r.persistClose(ctx, j, o, ""); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = db.DB.QueryRow("SELECT count(*) FROM jobs WHERE kind='payment' AND status='ready'").Scan(&count); e != nil || count != 1 {
		t.Fatal("local close was committed without its durable payment followup", count, e)
	}
}
func TestCloseDeletionDuringCloseQueryDoesNotRecreate(t *testing.T) {
	db, s, q, _, id := closeRaceSetup(t)
	ctx := context.Background()
	j, e := q.Claim(ctx, []string{"close"})
	if e != nil {
		t.Fatal(e)
	}
	g := &closeRaceGateway{query: func(o order.Order) (order.Observation, error) {
		if e := s.Delete(ctx, id, ""); e != nil {
			t.Fatal(e)
		}
		return closeRaceObservation(o, "unpaid"), nil
	}}
	r := Runner{Queue: q, Orders: s, Gateway: g}
	r.Execute(ctx, j)
	for _, table := range []string{"orders", "jobs", "payment_observations"} {
		var n int
		if e := db.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal(table, n, e)
		}
	}
	if g.closes != 0 {
		t.Fatal("closed remote deleted order")
	}
}
func TestCloseNotificationDuringCloseQueryStaysPaid(t *testing.T) {
	db, s, q, _, id := closeRaceSetup(t)
	ctx := context.Background()
	j, e := q.Claim(ctx, []string{"close"})
	if e != nil {
		t.Fatal(e)
	}
	g := &closeRaceGateway{query: func(o order.Order) (order.Observation, error) {
		paid := closeRaceObservation(o, "paid")
		paid.Source = "notify"
		if _, e := s.Apply(ctx, paid); e != nil {
			t.Fatal(e)
		}
		return closeRaceObservation(o, "unpaid"), nil
	}}
	r := Runner{Queue: q, Orders: s, Gateway: g}
	r.Execute(ctx, j)
	o, e := s.Get(ctx, id)
	if e != nil || o.Status != "paid" || g.closes != 0 {
		t.Fatal(o, e, g.closes)
	}
	var n int
	if e := db.DB.QueryRow("SELECT count(*) FROM jobs WHERE status IN ('ready','running')").Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}
func TestCloseExpiredCloseWorkerCannotWriteRemoteACK(t *testing.T) {
	_, s, q, now, id := closeRaceSetup(t)
	ctx := context.Background()
	j, e := q.Claim(ctx, []string{"close"})
	if e != nil {
		t.Fatal(e)
	}
	var replacement Job
	g := &closeRaceGateway{query: func(o order.Order) (order.Observation, error) {
		*now = now.Add(61 * time.Second)
		var e error
		replacement, e = q.Claim(ctx, []string{"close"})
		if e != nil {
			t.Fatal(e)
		}
		return closeRaceObservation(o, "unpaid"), nil
	}}
	r := Runner{Queue: q, Orders: s, Gateway: g}
	r.Execute(ctx, j)
	o, e := s.Get(ctx, id)
	if e != nil || o.Status != "closed" || o.RemoteCloseStatus == "confirmed" || g.closes != 0 {
		t.Fatal(o, e, g.closes)
	}
	got, e := q.Get(ctx, replacement.ID)
	if e != nil || got.Status != "running" || got.LeaseToken != replacement.LeaseToken {
		t.Fatal(got, e)
	}
}
func TestCloseFinalQueryFailureRecoversToPaid(t *testing.T) {
	_, s, q, now, id := closeRaceSetup(t)
	ctx := context.Background()
	calls := 0
	g := &closeRaceGateway{query: func(o order.Order) (order.Observation, error) {
		calls++
		if calls == 2 {
			return order.Observation{}, context.DeadlineExceeded
		}
		if calls >= 3 {
			return closeRaceObservation(o, "paid"), nil
		}
		return closeRaceObservation(o, "unpaid"), nil
	}}
	r := Runner{Queue: q, Orders: s, Gateway: g}
	j, e := q.Claim(ctx, []string{"close"})
	if e != nil {
		t.Fatal(e)
	}
	r.Execute(ctx, j)
	after, e := q.Get(ctx, j.ID)
	if e != nil || after.Status != "ready" || after.Result != "final_query_failed" {
		t.Fatal(after, e)
	}
	*now = after.RunAt.Add(time.Second)
	r.next = time.Time{}
	r.blocked = time.Time{}
	j, e = q.Claim(ctx, []string{"close"})
	if e != nil {
		t.Fatal(e)
	}
	r.Execute(ctx, j)
	o, e := s.Get(ctx, id)
	if e != nil || o.Status != "paid" || calls != 3 || g.closes != 1 {
		t.Fatal(o, e, calls, g.closes)
	}
	if _, e = q.Claim(ctx, []string{"close", "payment"}); e != sql.ErrNoRows {
		t.Fatal("paid job remained active", e)
	}
}
