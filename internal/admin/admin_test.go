package admin

import (
	"context"
	"errors"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestBanRevokesAllAndDoesNotRevive(t *testing.T) {
	ctx := context.Background()
	db, e := storage.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(db, config.Config{AdminIDs: map[string]bool{}}, l)
	admin, _ := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "issuer", Subject: "admin"})
	user, _ := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "issuer", Subject: "user"})
	a.Config.AdminIDs[admin.Session.UserID] = true
	s := New(db, order.New(db, l, "g", "p", "v2"), a.Config, l)
	if e = s.Ban(ctx, admin.Session.UserID, admin.Session.UserID, true); !errors.Is(e, ErrSelfBan) {
		t.Fatal("self ban", e)
	}
	if e = s.Ban(ctx, admin.Session.UserID, user.Session.UserID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Renew(ctx, user.Token); !errors.Is(e, auth.ErrBanned) {
		t.Fatal(e)
	}
	if e = s.Ban(ctx, admin.Session.UserID, user.Session.UserID, false); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Resolve(ctx, user.Token); !errors.Is(e, auth.ErrUnauthorized) {
		t.Fatal("old session revived", e)
	}
}
func TestPreviewCannotChangeSelectionAndExecuteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, e := storage.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(db, config.Config{}, l)
	c, _ := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "i", Subject: "a"})
	cfg := config.Config{AdminIDs: map[string]bool{c.Session.UserID: true}}
	o := order.New(db, l, "g", "p", "v2")
	v, e := o.Create(ctx, order.CreateInput{UserID: c.Session.UserID, AmountFen: 1, Channel: "alipay", Key: "one"})
	if e != nil {
		t.Fatal(e)
	}
	s := New(db, o, cfg, l)
	preview, e := s.Preview(ctx, c.Session.UserID, "delete", []string{v.ID, v.ID})
	if e != nil || len(preview.Items) != 1 {
		t.Fatal(e)
	}
	result, e := s.Execute(ctx, c.Session.UserID, preview.ID)
	if e != nil || result.Items[0].Result != "deleted" {
		t.Fatal(e, result)
	}
	again, e := s.Execute(ctx, c.Session.UserID, preview.ID)
	if e != nil || again.Items[0].Result != "deleted" {
		t.Fatal("repeat execute", e)
	}
	if _, e = o.Get(ctx, v.ID); !errors.Is(e, order.ErrNotFound) {
		t.Fatal(e)
	}
	var n int
	db.DB.QueryRow("SELECT count(*) FROM audit_events WHERE action='order.delete'").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
}
