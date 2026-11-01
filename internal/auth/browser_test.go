package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/storage"
)

func TestBrowserCurrentNeverFallsBackAfterInvalidation(t *testing.T) {
	for _, invalidation := range []string{"logout", "banned", "expired", "deleted", "missing-token"} {
		for _, sameAccount := range []bool{false, true} {
			t.Run(invalidation+map[bool]string{true: "/same-account", false: "/different-account"}[sameAccount], func(t *testing.T) {
				s := testAuth(t)
				ctx := context.Background()
				old, err := s.LoginIdentity(ctx, Identity{Provider: "oidc", Issuer: "test", Subject: "old"})
				if err != nil {
					t.Fatal(err)
				}
				subject := "new"
				if sameAccount {
					subject = "old"
				}
				current, err := s.LoginIdentity(WithBrowserFamilies(ctx, []string{old.Session.BrowserFamily}), Identity{Provider: "oidc", Issuer: "test", Subject: subject})
				if err != nil {
					t.Fatal(err)
				}
				if current.Session.ID == old.Session.ID || current.Session.CSRF == old.Session.CSRF {
					t.Fatal("new login reused credentials")
				}
				oldID, _ := storage.ParseID(old.Session.ID)
				// The persistent current check must work even if an old record is
				// retained without its eager revoked marker.
				if _, err := s.Store.DB.Exec("UPDATE sessions SET revoked_at=NULL WHERE id=?", oldID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Resolve(ctx, old.Token); !errors.Is(err, ErrUnauthorized) {
					t.Fatalf("superseded token resolved: %v", err)
				}
				if _, err := s.Renew(ctx, old.Token); !errors.Is(err, ErrUnauthorized) {
					t.Fatalf("superseded token renewed: %v", err)
				}
				id, _ := storage.ParseID(current.Session.ID)
				switch invalidation {
				case "logout":
					err = s.Logout(ctx, current.Token)
				case "banned":
					uid, _ := storage.ParseID(current.Session.UserID)
					_, err = s.Store.DB.Exec("UPDATE users SET status='banned' WHERE id=?", uid)
				case "expired":
					_, err = s.Store.DB.Exec("UPDATE sessions SET expires_at=1 WHERE id=?", id)
				case "deleted":
					_, err = s.Store.DB.Exec("DELETE FROM sessions WHERE id=?", id)
				}
				if err != nil {
					t.Fatal(err)
				}
				// A fresh Service shares only persisted state, as after a restart.
				restarted := New(s.Store, s.Config, s.Log)
				resolved, err := restarted.ResolveBrowser(ctx, []SessionCandidate{{ID: old.Session.ID, Token: old.Token}}, []string{old.Session.BrowserFamily})
				if !errors.Is(err, ErrUnauthorized) || resolved.Session != nil {
					t.Fatalf("old identity restored: %+v %v", resolved, err)
				}
				anchorOnly, err := restarted.ResolveBrowser(ctx, nil, []string{current.Session.BrowserFamily})
				if err != nil || anchorOnly.Session != nil || anchorOnly.Token != "" {
					t.Fatalf("anchor authenticated: %+v %v", anchorOnly, err)
				}
			})
		}
	}
}

func TestBrowserUnionRetainsDeletedCurrentWatermark(t *testing.T) {
	s := testAuth(t)
	ctx := context.Background()
	old, err := s.LoginIdentity(ctx, Identity{Provider: "oidc", Issuer: "test", Subject: "first-browser"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.LoginIdentity(ctx, Identity{Provider: "oidc", Issuer: "test", Subject: "second-browser"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := storage.ParseID(current.Session.ID)
	if _, err := s.Store.DB.Exec("DELETE FROM sessions WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	for _, families := range [][]string{{old.Session.BrowserFamily, current.Session.BrowserFamily}, {current.Session.BrowserFamily, old.Session.BrowserFamily}} {
		resolved, err := s.ResolveBrowser(ctx, []SessionCandidate{{ID: old.Session.ID, Token: old.Token}}, families)
		if !errors.Is(err, ErrUnauthorized) || resolved.Session != nil {
			t.Fatalf("union fell back from deleted current: %+v %v", resolved, err)
		}
	}
	// A delayed old alias returns after cleanup; it still reaches the same root.
	resolved, err := s.ResolveBrowser(ctx, []SessionCandidate{{Token: old.Token}}, []string{old.Session.BrowserFamily})
	if !errors.Is(err, ErrUnauthorized) || resolved.FamilyID != current.Session.BrowserFamily {
		t.Fatalf("alias split after merge: %+v %v", resolved, err)
	}
}

func TestBrowserIssuanceOrderAndSlidingExpiry(t *testing.T) {
	s := testAuth(t)
	now := time.Now().UTC().Truncate(time.Second)
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	c, err := s.LoginIdentity(ctx, Identity{Provider: "oidc", Issuer: "test", Subject: "active"})
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		now = now.Add(10 * 24 * time.Hour)
		renewed, changed, err := s.RenewIfNeeded(ctx, c.Token)
		if err != nil || !changed || !renewed.ExpiresAt.Equal(now.Add(IdleTTL)) {
			t.Fatalf("active session stopped sliding: %+v %v %v", renewed, changed, err)
		}
	}
	next, err := s.LoginIdentity(WithBrowserFamilies(ctx, []string{c.Session.BrowserFamily}), Identity{Provider: "oidc", Issuer: "test", Subject: "active"})
	if err != nil {
		t.Fatal(err)
	}
	var oldOrder, nextOrder int64
	oldID, _ := storage.ParseID(c.Session.ID)
	nextID, _ := storage.ParseID(next.Session.ID)
	if err := s.Store.DB.QueryRow("SELECT issued_order FROM sessions WHERE id=?", oldID).Scan(&oldOrder); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.DB.QueryRow("SELECT issued_order FROM sessions WHERE id=?", nextID).Scan(&nextOrder); err != nil {
		t.Fatal(err)
	}
	if nextOrder <= oldOrder {
		t.Fatal("issuance order is not monotonic")
	}
}
