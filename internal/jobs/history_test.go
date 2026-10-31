package jobs

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"github.com/google/uuid"
)

func TestHistoryPersistsSafeDiagnosticOnRetry(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC().Truncate(time.Second)
	oid := storage.NewID()
	if _, err = db.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'diagnostic',1,'alipay',?,?,'192.0.2.1')", oid[:], now.Unix(), now.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	q := NewQueue(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	q.Now = func() time.Time { return now }
	_, err = q.Enqueue(ctx, "payment", oid.String(), "automatic", "", "diagnostic")
	if err != nil {
		t.Fatal(err)
	}
	j, err := q.Claim(ctx, []string{"payment"})
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Queue: q}
	r.retry(ctx, j, order.Order{ID: oid.String(), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, "query_failed", context.DeadlineExceeded)
	bid, _ := storage.ParseID(j.ID)
	var message string
	if err = db.DB.QueryRow("SELECT message FROM task_events WHERE job_id=? ORDER BY sequence DESC LIMIT 1", bid).Scan(&message); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "timeout") || !strings.Contains(message, strings.ReplaceAll(j.ID, "-", "")) || !strings.Contains(message, strings.ReplaceAll(oid.String(), "-", "")) {
		t.Fatalf("missing diagnostic or IDs: %s", message)
	}
}

// Missing transactional observation must fail these tests, including direct SQL
// writes made by callbacks and admin geo refresh rather than Queue.Finish.
func TestHistoryTracksLeaseRecoveryAndDirectCompletion(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Unix(1800000000, 0).UTC()
	oid := storage.NewID()
	if _, err = db.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'history',1,'alipay',?,?,'192.0.2.1')", oid[:], now.Unix(), now.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	q := NewQueue(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	q.Now = func() time.Time { return now }
	id, err := q.Enqueue(ctx, "payment", oid.String(), "automatic", "", "history-request")
	if err != nil {
		t.Fatal(err)
	}
	first, err := q.Claim(ctx, []string{"payment"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(61 * time.Second)
	second, err := q.Claim(ctx, []string{"payment"})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := q.Finish(ctx, first, "failed", "stale", time.Time{}); err != nil || changed {
		t.Fatal(changed, err)
	}
	bid, _ := storage.ParseID(id)
	if _, err = db.DB.Exec("UPDATE jobs SET status='done',lease_token=NULL,lease_until=NULL,last_result='paid',updated_at=? WHERE id=?", now.Unix(), bid); err != nil {
		t.Fatal(err)
	}
	if changed, err := q.Finish(ctx, second, "failed", "stale", time.Time{}); err != nil || changed {
		t.Fatal(changed, err)
	}
	rows, err := db.DB.Query("SELECT id,event,attempt,created_at,message FROM task_events WHERE job_id=? ORDER BY sequence", bid)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"created", "started", "recovered", "completed"}
	var got []string
	for rows.Next() {
		var eid []byte
		var event, message string
		var attempt int
		var at int64
		if err = rows.Scan(&eid, &event, &attempt, &at, &message); err != nil {
			t.Fatal(err)
		}
		u, e := uuid.FromBytes(eid)
		if e != nil || u.Version() != 4 || u.Variant() != uuid.RFC4122 {
			t.Fatalf("invalid event UUID: %x", eid)
		}
		if len(message) == 0 {
			t.Fatal("empty event message")
		}
		got = append(got, event)
		if len(got) > 1 && attempt != len(got)-1 && event != "completed" {
			t.Fatalf("attempt %d", attempt)
		}
		if event == "recovered" && at != now.Unix() {
			t.Fatal("history lost business clock", at)
		}
	}
	rows.Close()
	if len(got) != len(want) {
		t.Fatalf("events=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events=%v want=%v", got, want)
		}
	}
	var total int
	if err = db.DB.QueryRow("SELECT total_attempts FROM jobs WHERE id=?", bid).Scan(&total); err != nil || total != 2 {
		t.Fatal(total, err)
	}
	rollback := errors.New("rollback")
	err = db.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.Exec("UPDATE jobs SET status='failed',updated_at=? WHERE id=?", now.Unix(), bid)
		if e != nil {
			return e
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var n int
	if err = db.DB.QueryRow("SELECT count(*) FROM task_events WHERE job_id=?", bid).Scan(&n); err != nil || n != 4 {
		t.Fatal(n, err)
	}
	if _, err = db.DB.Exec("DELETE FROM orders WHERE id=?", oid[:]); err != nil {
		t.Fatal(err)
	}
	if err = db.DB.QueryRow("SELECT count(*) FROM task_events WHERE job_id=?", bid).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
