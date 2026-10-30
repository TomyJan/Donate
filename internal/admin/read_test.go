package admin

import (
	"bytes"
	"context"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloseQueueSortExportAndCurrentAudit(t *testing.T) {
	ctx := context.Background()
	db, e := storage.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(db, config.Config{}, l)
	c, _ := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "i", Subject: "s", Nickname: "Someone"})
	cfg := config.Config{AdminIDs: map[string]bool{c.Session.UserID: true}}
	orders := order.New(db, l, "g", "p", "v2")
	o, e := orders.Create(ctx, order.CreateInput{UserID: c.Session.UserID, AmountFen: 888, Channel: "alipay", Message: "=HYPERLINK(\"evil\")", IP: "203.0.113.42", Key: "one"})
	if e != nil {
		t.Fatal(e)
	}
	s := New(db, orders, cfg, l)
	op, e := s.Preview(ctx, c.Session.UserID, "close", []string{o.ID})
	if e != nil {
		t.Fatal(e)
	}
	op, e = s.Execute(ctx, c.Session.UserID, op.ID)
	if e != nil || op.Items[0].Result != "queued" {
		t.Fatalf("close %+v %v", op, e)
	}
	page, e := s.OrdersList(ctx, Filter{Search: "Someone", UserID: c.Session.UserID, Sort: "created_at; DROP TABLE orders;", Size: 20})
	if e != nil || len(page.Items) != 1 {
		t.Fatal("filter", e)
	}
	op, e = s.Preview(ctx, c.Session.UserID, "export", []string{o.ID})
	if e != nil {
		t.Fatal(e)
	}
	op, e = s.Execute(ctx, c.Session.UserID, op.ID)
	if e != nil {
		t.Fatal(e)
	}
	var csv bytes.Buffer
	if e = s.Export(ctx, c.Session.UserID, op.ID, &csv); e != nil || !strings.Contains(csv.String(), "'=HYPERLINK") {
		t.Fatal("CSV injection", e, csv.String())
	}
	if strings.Contains(csv.String(), "203.0.113.42") || strings.Contains(csv.String(), ",IP") {
		t.Fatal("CSV must not export full IP addresses", csv.String())
	}
	if e = orders.Delete(ctx, o.ID, c.Session.UserID); e != nil {
		t.Fatal(e)
	}
	audit, e := s.Audit(ctx, Filter{Size: 20})
	if e != nil {
		t.Fatal(e)
	}
	for _, event := range audit.Items {
		if event.TargetExists || event.Target != "" || event.TargetAmountFen != nil || event.TargetNickname != "" || strings.Contains(event.Target, "HYPERLINK") {
			t.Fatal(event)
		}
	}
}

func TestOrderSearchFindsUserID(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(db, config.Config{}, logger)
	owner, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "owner", Nickname: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "other", Nickname: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	orders := order.New(db, logger, "g", "p", "v2")
	want, err := orders.Create(ctx, order.CreateInput{UserID: owner.Session.UserID, AmountFen: 888, Channel: "alipay", Key: "owner-order"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = orders.Create(ctx, order.CreateInput{UserID: other.Session.UserID, AmountFen: 888, Channel: "alipay", Key: "other-order"}); err != nil {
		t.Fatal(err)
	}
	s := New(db, orders, config.Config{}, logger)
	page, err := s.OrdersList(ctx, Filter{Search: owner.Session.UserID})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != want.ID {
		t.Fatalf("user ID search returned %+v, error %v", page, err)
	}
}
