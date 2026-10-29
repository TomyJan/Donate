package order

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/storage"
)

func setup(t *testing.T) (*Service, string) {
	t.Helper()
	db, e := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(db, config.Config{AdminIDs: map[string]bool{}}, log)
	c, e := a.LoginIdentity(context.Background(), auth.Identity{Provider: "oidc", Issuer: "issuer", Subject: "one", Nickname: "甲"})
	if e != nil {
		t.Fatal(e)
	}
	return New(db, log, "https://gateway.test", "1001", "v2"), c.Session.UserID
}
func create(t *testing.T, s *Service, user, key string) Order {
	t.Helper()
	v, e := s.Create(context.Background(), CreateInput{UserID: user, AmountFen: 888, Channel: "alipay", Message: "支持", IP: "192.0.2.1", Key: key})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestCreateRetryIsIdempotent(t *testing.T) {
	s, u := setup(t)
	a, b := create(t, s, u, "same"), create(t, s, u, "same")
	if a.ID != b.ID {
		t.Fatal("duplicate orders")
	}
	if _, e := s.Create(context.Background(), CreateInput{UserID: u, AmountFen: 889, Channel: "alipay", Key: "same"}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
}
func TestPaymentConcurrentAndNoDowngrade(t *testing.T) {
	s, u := setup(t)
	o := create(t, s, u, "paid")
	obs := Observation{MerchantOrderNo: o.MerchantOrderNo, MerchantID: "1001", GatewayID: "https://gateway.test", TradeNo: "gateway-1", Channel: "alipay", AmountFen: 888, State: "paid", Verified: true, Source: "notify", PaidAt: time.Now()}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Go(func() { _, e := s.Apply(context.Background(), obs); errs <- e })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	obs.State = "unpaid"
	if _, e := s.Apply(context.Background(), obs); e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(context.Background(), o.ID)
	if e != nil || got.Status != "paid" {
		t.Fatal("paid downgraded", e)
	}
	var n int
	s.Store.DB.QueryRow("SELECT count(*) FROM audit_events WHERE action='order.paid'").Scan(&n)
	if n != 1 {
		t.Fatal("duplicate business event", n)
	}
}
func TestPaymentMismatchAndDeletionCannotResurrect(t *testing.T) {
	s, u := setup(t)
	o := create(t, s, u, "delete")
	obs := Observation{MerchantOrderNo: o.MerchantOrderNo, MerchantID: "1001", GatewayID: "https://gateway.test", TradeNo: "gateway-2", Channel: "alipay", AmountFen: 889, State: "paid", Verified: true, Source: "query"}
	if _, e := s.Apply(context.Background(), obs); !errors.Is(e, ErrMismatch) {
		t.Fatal("wrong amount accepted", e)
	}
	if e := s.Delete(context.Background(), o.ID, u); e != nil {
		t.Fatal(e)
	}
	obs.AmountFen = 888
	if _, e := s.Apply(context.Background(), obs); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted order rebuilt", e)
	}
}
func TestLatePaymentAcceptedAfterExpiry(t *testing.T) {
	s, u := setup(t)
	o := create(t, s, u, "late")
	s.Now = func() time.Time { return o.CreatedAt.Add(31 * time.Minute) }
	if e := s.Expire(context.Background()); e != nil {
		t.Fatal(e)
	}
	_, e := s.Apply(context.Background(), Observation{MerchantOrderNo: o.MerchantOrderNo, MerchantID: "1001", GatewayID: "https://gateway.test", TradeNo: "late", Channel: "alipay", AmountFen: 888, State: "paid", Verified: true, Source: "query"})
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(context.Background(), o.ID)
	if e != nil || got.Status != "paid" {
		t.Fatal(e)
	}
}

func TestExpiredWorkerCannotApplyPayment(t *testing.T) {
	s, u := setup(t)
	o := create(t, s, u, "fenced")
	id, _ := storage.ParseID(o.ID)
	var jid []byte
	if e := s.Store.DB.QueryRow("UPDATE jobs SET status='running',lease_token='current',lease_until=? WHERE kind='payment' AND order_id=? RETURNING id", time.Now().Add(time.Minute).Unix(), id).Scan(&jid); e != nil {
		t.Fatal(e)
	}
	obs := Observation{MerchantOrderNo: o.MerchantOrderNo, MerchantID: "1001", GatewayID: "https://gateway.test", TradeNo: "fenced", Channel: "alipay", AmountFen: 888, State: "paid", Verified: true, Source: "query", JobID: storage.IDString(jid), LeaseToken: "old"}
	if _, e := s.Apply(context.Background(), obs); !errors.Is(e, ErrBusy) {
		t.Fatal("stale worker accepted", e)
	}
	got, _ := s.Get(context.Background(), o.ID)
	if got.Status != "pending" {
		t.Fatal(got.Status)
	}
	obs.LeaseToken = "current"
	if _, e := s.Apply(context.Background(), obs); e != nil {
		t.Fatal(e)
	}
}

func TestWorkerLeaseCannotApplyAnotherOrder(t *testing.T) {
	s, u := setup(t)
	first := create(t, s, u, "lease-owner")
	other := create(t, s, u, "lease-other")
	oid, _ := storage.ParseID(first.ID)
	var jid []byte
	if e := s.Store.DB.QueryRow("UPDATE jobs SET status='running',lease_token='current',lease_until=? WHERE kind='payment' AND order_id=? RETURNING id", time.Now().Add(time.Minute).Unix(), oid).Scan(&jid); e != nil {
		t.Fatal(e)
	}
	obs := Observation{MerchantOrderNo: other.MerchantOrderNo, MerchantID: "1001", GatewayID: "https://gateway.test", TradeNo: "wrong-lease", Channel: "alipay", AmountFen: 888, State: "paid", Verified: true, Source: "query", JobID: storage.IDString(jid), LeaseToken: "current"}
	if _, e := s.Apply(context.Background(), obs); !errors.Is(e, ErrBusy) {
		t.Fatalf("another order accepted: %v", e)
	}
	got, _ := s.Get(context.Background(), other.ID)
	if got.Status != "pending" {
		t.Fatal(got.Status)
	}
}
