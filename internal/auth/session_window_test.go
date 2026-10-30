package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRenewOnlyInsideTwentyDayWindow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		advance time.Duration
		writes  int
	}{
		{"outside by one second", 10*24*time.Hour - time.Second, 0},
		{"exact threshold", 10 * 24 * time.Hour, 1},
		{"inside window", 11 * 24 * time.Hour, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testAuth(t)
			now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			s.Now = func() time.Time { return now }
			ctx := context.Background()
			c, err := s.LoginIdentity(ctx, Identity{Provider: "oidc", Issuer: "test", Subject: "window"})
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewTextHandler(&logs, nil))
			if _, err = s.Store.DB.Exec("CREATE TABLE renewal_updates(n INTEGER); INSERT INTO renewal_updates VALUES(0); CREATE TRIGGER count_renewal AFTER UPDATE OF expires_at ON sessions BEGIN UPDATE renewal_updates SET n=n+1; END"); err != nil {
				t.Fatal(err)
			}
			now = now.Add(tc.advance)
			for range 3 {
				got, err := s.Renew(ctx, c.Token)
				if err != nil {
					t.Fatal(err)
				}
				want := c.Session.ExpiresAt
				if tc.writes == 1 {
					want = now.Add(30 * 24 * time.Hour)
				}
				if !got.ExpiresAt.Equal(want) {
					t.Fatalf("expiry = %s, want %s", got.ExpiresAt, want)
				}
			}
			var writes int
			var renewed int64
			if err := s.Store.DB.QueryRow("SELECT n FROM renewal_updates").Scan(&writes); err != nil {
				t.Fatal(err)
			}
			if err := s.Store.DB.QueryRow("SELECT renewed_at FROM sessions").Scan(&renewed); err != nil {
				t.Fatal(err)
			}
			wantRenewed := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
			if tc.writes == 1 {
				wantRenewed = now.Unix()
			}
			if writes != tc.writes || renewed != wantRenewed {
				t.Fatalf("writes=%d renewed=%d, want %d/%d", writes, renewed, tc.writes, wantRenewed)
			}
			if got := strings.Count(logs.String(), "已续期"); got != tc.writes {
				t.Fatalf("renewal logs=%d, want %d", got, tc.writes)
			}
		})
	}
}

func TestConcurrentThresholdRenewalWritesOnce(t *testing.T) {
	s := testAuth(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	c, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: "test", Subject: "concurrent-window"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * 24 * time.Hour)
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewTextHandler(&logs, nil))
	if _, err = s.Store.DB.Exec("CREATE TABLE renewal_updates(n INTEGER); INSERT INTO renewal_updates VALUES(0); CREATE TRIGGER count_renewal AFTER UPDATE OF expires_at ON sessions BEGIN UPDATE renewal_updates SET n=n+1; END"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 12 {
		wg.Go(func() {
			<-start
			got, err := s.Renew(context.Background(), c.Token)
			if err != nil {
				t.Error(err)
				return
			}
			if !got.ExpiresAt.Equal(now.Add(30 * 24 * time.Hour)) {
				t.Errorf("expiry=%s", got.ExpiresAt)
			}
		})
	}
	close(start)
	wg.Wait()
	var writes int
	if err := s.Store.DB.QueryRow("SELECT n FROM renewal_updates").Scan(&writes); err != nil {
		t.Fatal(err)
	}
	if writes != 1 || strings.Count(logs.String(), "已续期") != 1 {
		t.Fatalf("writes=%d logs=%s", writes, logs.String())
	}
}

func TestRenewRejectsExpiredRevokedAndBannedSessions(t *testing.T) {
	for _, kind := range []string{"expired", "revoked", "banned"} {
		t.Run(kind, func(t *testing.T) {
			s := testAuth(t)
			now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			s.Now = func() time.Time { return now }
			c, err := s.LoginIdentity(context.Background(), Identity{Provider: "oidc", Issuer: "test", Subject: kind})
			if err != nil {
				t.Fatal(err)
			}
			want := ErrUnauthorized
			switch kind {
			case "expired":
				now = now.Add(30 * 24 * time.Hour)
			case "revoked":
				_, err = s.Store.DB.Exec("UPDATE sessions SET revoked_at=?", now.Unix())
			case "banned":
				_, err = s.Store.DB.Exec("UPDATE users SET status='banned'")
				want = ErrBanned
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Renew(context.Background(), c.Token); !errors.Is(err, want) {
				t.Fatalf("error=%v, want %v", err, want)
			}
			var expiry int64
			if err := s.Store.DB.QueryRow("SELECT expires_at FROM sessions").Scan(&expiry); err != nil {
				t.Fatal(err)
			}
			if expiry != c.Session.ExpiresAt.Unix() {
				t.Fatal("invalid session expiry changed")
			}
		})
	}
}
