package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func readFixture(t *testing.T) (*Service, string) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "read.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	login, err := auth.New(db, config.Config{}, logger).LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "test", Subject: "owner", Nickname: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, order.New(db, logger, "g", "p", "v2"), config.Config{AdminIDs: map[string]bool{login.Session.UserID: true}}, logger)
	s.Now = func() time.Time { return time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC) }
	return s, login.Session.UserID
}

func addReadOrder(t *testing.T, s *Service, user, key, status, created, paid, channel, message string, amount int64, hidden bool) order.Order {
	t.Helper()
	ctx := context.Background()
	o, err := s.Orders.Create(ctx, order.CreateInput{UserID: user, Key: key, AmountFen: amount, Channel: channel, Message: message})
	if err != nil {
		t.Fatal(err)
	}
	createdAt, err := time.Parse(time.RFC3339, created)
	if err != nil {
		t.Fatal(err)
	}
	var paidAt any
	var trade any
	if paid != "" {
		at, err := time.Parse(time.RFC3339, paid)
		if err != nil {
			t.Fatal(err)
		}
		paidAt = at.Unix()
		trade = key
	}
	oid, _ := storage.ParseID(o.ID)
	if _, err = s.Store.DB.ExecContext(ctx, "UPDATE orders SET status=?,created_at=?,paid_at=?,trade_no=?,hidden_order=?,hidden_message=? WHERE id=?", status, createdAt.Unix(), paidAt, trade, hidden, hidden, oid); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOverviewDefaultUsesPaidAndCreatedCalendarWindows(t *testing.T) {
	s, user := readFixture(t)
	// Default window is [October 1, October 31) in Shanghai, including today.
	addReadOrder(t, s, user, "old", "paid", "2026-09-01T00:00:00+08:00", "2026-09-30T23:59:59+08:00", "alipay", "", 100, false)
	addReadOrder(t, s, user, "lower", "paid", "2026-09-01T00:00:00+08:00", "2026-10-01T00:00:00+08:00", "alipay", "", 200, false)
	addReadOrder(t, s, user, "hidden", "paid", "2026-09-01T00:00:00+08:00", "2026-10-30T23:59:59+08:00", "wxpay", "", 300, true)
	addReadOrder(t, s, user, "upper", "paid", "2026-10-30T00:00:00+08:00", "2026-10-31T00:00:00+08:00", "wxpay", "", 400, false)
	removed := addReadOrder(t, s, user, "deleted", "paid", "2026-10-02T00:00:00+08:00", "2026-10-02T00:00:00+08:00", "alipay", "", 500, false)
	if err := s.Orders.Delete(context.Background(), removed.ID, user); err != nil {
		t.Fatal(err)
	}
	addReadOrder(t, s, user, "pending-old", "pending", "2026-09-30T23:59:59+08:00", "", "alipay", "", 1, false)
	addReadOrder(t, s, user, "pending-current", "pending", "2026-10-01T00:00:00+08:00", "", "alipay", "", 1, true)
	addReadOrder(t, s, user, "closed-current", "closed", "2026-10-30T23:59:59+08:00", "", "wxpay", "", 1, false)
	addReadOrder(t, s, user, "closed-future", "closed", "2026-10-31T00:00:00+08:00", "", "wxpay", "", 1, false)
	got, err := s.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.PaidFen != 500 || got.Paid != 2 || got.Pending != 1 || got.Closed != 1 {
		t.Fatalf("overview=%+v", got)
	}
	for name, metrics := range map[string][]Metric{"daily": got.Daily, "channels": got.Channels} {
		var sum int64
		var count int
		for _, m := range metrics {
			sum += m.PaidFen
			count += m.Count
		}
		if sum != 500 || count != 2 {
			t.Errorf("%s totals=%d/%d, want 500/2", name, sum, count)
		}
	}
	if len(got.Daily) != 2 || got.Daily[0].Label != "2026-10-01" || got.Daily[1].Label != "2026-10-30" {
		t.Errorf("daily=%+v", got.Daily)
	}
}

func TestOrdersListSortsMessage(t *testing.T) {
	s, user := readFixture(t)
	z := addReadOrder(t, s, user, "z", "pending", "2026-10-01T00:00:00Z", "", "alipay", "zebra", 1, false)
	a := addReadOrder(t, s, user, "a", "pending", "2026-10-02T00:00:00Z", "", "alipay", "apple", 1, false)
	got, err := s.OrdersList(context.Background(), Filter{Sort: "message", Direction: "asc", Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Items[0].ID != a.ID || got.Items[1].ID != z.ID {
		t.Fatalf("message order=%+v", got.Items)
	}
}

func TestOrdersListAndSelectedExportPreserveExactTimeBounds(t *testing.T) {
	s, user := readFixture(t)
	ctx := context.Background()
	ids := []string{}
	for i, at := range []string{"2026-10-30T09:59:57Z", "2026-10-30T09:59:58Z", "2026-10-30T09:59:59Z"} {
		o := addReadOrder(t, s, user, at, "closed", at, "", "alipay", "", int64(i+1), false)
		ids = append(ids, o.ID)
	}
	for _, tc := range []struct {
		name, from, to string
		want           []string
	}{
		{"from-only", "2026-10-30T09:59:57.5Z", "", ids[1:]},
		{"to-only", "", "2026-10-30T09:59:58.5Z", ids[:2]},
		{"fractional-half-open", "2026-10-30T09:59:57.5Z", "2026-10-30T09:59:58.5Z", ids[1:2]},
		{"whole-second-half-open", "2026-10-30T09:59:58Z", "2026-10-30T09:59:59Z", ids[1:2]},
		{"nanosecond-bounds", "2026-10-30T09:59:57.000000001Z", "2026-10-30T09:59:58.000000001Z", ids[1:2]},
		{"offset-timezone", "2026-10-30T17:59:57.5+08:00", "2026-10-30T17:59:58.5+08:00", ids[1:2]},
		{"valid-subsecond-empty-range", "2026-10-30T09:59:57.25Z", "2026-10-30T09:59:57.75Z", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.OrdersList(ctx, Filter{From: tc.from, To: tc.to, Sort: "created_at", Direction: "asc", Size: 20})
			if err != nil {
				t.Fatal(err)
			}
			selected := []string{}
			for _, item := range page.Items {
				selected = append(selected, item.ID)
			}
			if page.Total != len(tc.want) || !slices.Equal(selected, tc.want) {
				t.Fatalf("range [%s,%s): got %v (total %d), want %v", tc.from, tc.to, selected, page.Total, tc.want)
			}
			if len(selected) == 0 {
				return
			}
			// The UI exports the selected filtered IDs through the ordinary confirmed
			// operation protocol; CSV must contain exactly that filtered selection.
			op, err := s.Preview(ctx, user, "export", selected)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Execute(ctx, user, op.ID); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err = s.Export(ctx, user, op.ID, &output); err != nil {
				t.Fatal(err)
			}
			rows, err := csv.NewReader(&output).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			exported := []string{}
			for _, row := range rows[1:] {
				exported = append(exported, row[0])
			}
			want := slices.Clone(tc.want)
			slices.Sort(want)
			slices.Sort(exported)
			if !slices.Equal(exported, want) {
				t.Fatalf("CSV IDs %v, want %v", exported, want)
			}
		})
	}
	for _, to := range []string{"2026-10-30T09:59:57.5Z", "2026-10-30T09:59:57.25Z"} {
		if _, err := s.OrdersList(ctx, Filter{From: "2026-10-30T09:59:57.5Z", To: to}); !errors.Is(err, auth.ErrInvalid) {
			t.Fatalf("non-increasing exact bounds accepted: to=%s err=%v", to, err)
		}
	}
}

func TestAuditTargetsUseCurrentReadableDataAndExistence(t *testing.T) {
	s, user := readFixture(t)
	ctx := context.Background()
	o := addReadOrder(t, s, user, "audit", "pending", "2026-10-01T00:00:00Z", "", "alipay", "private message", 888, false)
	oid, _ := storage.ParseID(o.ID)
	uid, _ := storage.ParseID(user)
	if err := s.Store.Write(ctx, func(tx *sql.Tx) error {
		if err := storage.Audit(ctx, tx, nil, "order.test", "order", oid, "ok", s.Now().Unix()); err != nil {
			return err
		}
		return storage.Audit(ctx, tx, nil, "user.test", "user", uid, "ok", s.Now().Unix())
	}); err != nil {
		t.Fatal(err)
	}
	wantNickname, wantSummary, wantAmount := "Alice", "Alice · ¥8.88", float64(888)
	check := func(wantExists bool) {
		t.Helper()
		result, err := s.Audit(ctx, Filter{Size: 20})
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range result.Items {
			if event.Action != "order.test" && event.Action != "user.test" {
				continue
			}
			raw, _ := json.Marshal(event)
			var data map[string]any
			_ = json.Unmarshal(raw, &data)
			if event.Actor != "system" {
				t.Errorf("actor=%q, want system", event.Actor)
			}
			exists, ok := data["target_exists"].(bool)
			if !ok {
				t.Fatalf("missing target_exists: %s", raw)
			}
			if event.TargetType == "user" {
				if !exists || event.Target != wantNickname {
					t.Errorf("user=%s", raw)
				}
				continue
			}
			if exists != wantExists {
				t.Errorf("target_exists=%v, want %v", exists, wantExists)
			}
			if wantExists {
				if event.Target != wantSummary || data["target_nickname"] != wantNickname || data["target_amount_fen"] != wantAmount {
					t.Errorf("target=%s", raw)
				}
			} else if event.Target != "" || data["target_amount_fen"] != nil || data["target_nickname"] != nil {
				t.Errorf("deleted target retained data: %s", raw)
			}
		}
	}
	check(true)
	// Audit reads current records; changing them cannot leave an old display snapshot.
	if _, err := s.Store.DB.ExecContext(ctx, "UPDATE identities SET nickname='Renamed' WHERE user_id=?", uid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.DB.ExecContext(ctx, "UPDATE orders SET amount_fen=999 WHERE id=?", oid); err != nil {
		t.Fatal(err)
	}
	wantNickname, wantSummary, wantAmount = "Renamed", "Renamed · ¥9.99", float64(999)
	check(true)
	if err := s.Orders.Delete(ctx, o.ID, user); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestAuditOrderSurvivesMissingUserJoin(t *testing.T) {
	s, user := readFixture(t)
	ctx := context.Background()
	o := addReadOrder(t, s, user, "anonymous", "pending", "2026-10-01T00:00:00Z", "", "wxpay", "", 1, false)
	oid, _ := storage.ParseID(o.ID)
	if _, err := s.Store.DB.ExecContext(ctx, "UPDATE orders SET user_id=NULL WHERE id=?", oid); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Write(ctx, func(tx *sql.Tx) error {
		return storage.Audit(ctx, tx, nil, "order.test", "order", oid, "ok", s.Now().Unix())
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Audit(ctx, Filter{Size: 20, Action: "order.test"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got.Items)
	var items []map[string]any
	_ = json.Unmarshal(raw, &items)
	if len(items) != 1 || items[0]["target_exists"] != true || items[0]["target_amount_fen"] != float64(1) {
		t.Fatalf("orphaned order=%s", raw)
	}
}

func TestOverviewLongRangesKeepEveryPaidDay(t *testing.T) {
	s, user := readFixture(t)
	last, _ := time.Parse(time.RFC3339, "2026-10-30T12:00:00+08:00")
	for day := 0; day < 95; day++ {
		at := last.AddDate(0, 0, -day).Format(time.RFC3339)
		addReadOrder(t, s, user, at, "paid", at, at, "alipay", "", 1, false)
	}
	for _, tc := range []struct {
		name string
		days int
	}{{"90d", 90}, {"all", 95}} {
		got, err := s.OverviewRange(context.Background(), tc.name, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Daily) != tc.days || got.Paid != tc.days || got.PaidFen != int64(tc.days) {
			t.Errorf("%s has %d daily points and totals %d/%d, want %d", tc.name, len(got.Daily), got.Paid, got.PaidFen, tc.days)
		}
	}
}

func TestAuditMissingUserTargetIsUnavailable(t *testing.T) {
	s, _ := readFixture(t)
	id := storage.NewID()
	err := s.Store.Write(context.Background(), func(tx *sql.Tx) error {
		return storage.Audit(context.Background(), tx, nil, "user.test", "user", id[:], "ok", s.Now().Unix())
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Audit(context.Background(), Filter{Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].TargetExists || got.Items[0].Target != "" || got.Items[0].TargetAmountFen != nil || got.Items[0].TargetNickname != "" {
		t.Fatalf("missing user target=%+v", got.Items)
	}
}
