package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestConcurrentRenewalCannotResurrectLoggedOutSession(t *testing.T) {
	s := testAuth(t)
	ctx := context.Background()
	c, e := s.LoginIdentity(ctx, Identity{Provider: "oidc", Issuer: "test", Subject: "race"})
	if e != nil {
		t.Fatal(e)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-start
			_, err := s.Renew(ctx, c.Token)
			if err != nil && !errors.Is(err, ErrUnauthorized) {
				t.Error(err)
			}
		})
	}
	wg.Go(func() {
		<-start
		if err := s.Logout(ctx, c.Token); err != nil {
			t.Error(err)
		}
	})
	close(start)
	wg.Wait()
	if _, e = s.Resolve(ctx, c.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("logout was undone", e)
	}
	if _, e = s.Renew(ctx, c.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("revoked credential renewed", e)
	}
}

func testAuth(t *testing.T) *Service {
	t.Helper()
	db, e := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, config.Config{AdminIDs: map[string]bool{}, Secret: make([]byte, 32)}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
func TestActiveSessionHasNoAbsoluteExpiry(t *testing.T) {
	s := testAuth(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	creds, e := s.LoginIdentity(ctx, Identity{Provider: "oidc", Issuer: "https://issuer.test", Subject: "one", Nickname: "甲"})
	if e != nil {
		t.Fatal(e)
	}
	for range 8 {
		now = now.Add(20 * 24 * time.Hour)
		if _, e = s.Renew(ctx, creds.Token); e != nil {
			t.Fatalf("active session lost: %v", e)
		}
	}
	now = now.Add(30 * 24 * time.Hour)
	if _, e = s.Resolve(ctx, creds.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("idle session did not expire", e)
	}
}
func TestRevokedSessionCannotRenew(t *testing.T) {
	s := testAuth(t)
	ctx := context.Background()
	c, e := s.LoginIdentity(ctx, Identity{Provider: "qq", Issuer: "app", Subject: "openid", Nickname: "乙", QQNumber: "123456"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Logout(ctx, c.Token); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Renew(ctx, c.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
}
func TestBindingConflictDoesNotMergeUsers(t *testing.T) {
	s := testAuth(t)
	ctx := context.Background()
	a, e := s.LoginIdentity(ctx, Identity{Provider: "oidc", Issuer: "issuer", Subject: "a", Nickname: "A"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.LoginIdentity(ctx, Identity{Provider: "qq", Issuer: "app", Subject: "b", Nickname: "B", QQNumber: "123456"})
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.Resolve(ctx, a.Token)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindIdentity(ctx, session.ID, Identity{Provider: "qq", Issuer: "app", Subject: "b", Nickname: "B", QQNumber: "123456"}); !errors.Is(e, ErrConflict) {
		t.Fatal("binding merged identities", e)
	}
	bs, e := s.Resolve(ctx, b.Token)
	if e != nil {
		t.Fatal(e)
	}
	if bs.UserID == session.UserID {
		t.Fatal("users merged")
	}
}
