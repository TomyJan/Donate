package jobs

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func runnerReadFixture(t *testing.T, kind string) (*Runner, Job, *time.Time) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "read.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	oid := storage.NewID()
	if _, err = db.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'read-existing',1,'alipay',?,?,'192.0.2.1')", oid[:], now.Unix(), now.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := NewQueue(db, l)
	q.Now = func() time.Time { return now }
	if _, err = q.Enqueue(ctx, kind, oid.String(), "automatic", "", "read-request"); err != nil {
		t.Fatal(err)
	}
	j, err := q.Claim(ctx, []string{kind})
	if err != nil {
		t.Fatal(err)
	}
	return &Runner{Queue: q, Orders: order.New(db, l, "", "", "v2"), Gateway: &fakeGateway{}}, j, &now
}

// Treating any Get error as ErrNotFound would permanently complete all of
// these real tasks once finish salvages an already canceled context.
func TestCanceledExecutePreservesExistingTaskAndLeaseRecovery(t *testing.T) {
	for _, kind := range []string{"payment", "close", "geo"} {
		for _, mode := range []string{"canceled", "deadline"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				r, j, now := runnerReadFixture(t, kind)
				parent, cancel := context.WithCancel(context.Background())
				if mode == "deadline" {
					cancel()
					parent, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				}
				cancel()
				r.Execute(parent, j)
				got, err := r.Queue.Get(context.Background(), j.ID)
				if err != nil || got.Status != "running" || got.Result != "" || got.LeaseToken != j.LeaseToken {
					t.Fatalf("existing order was completed after %s: %+v %v", mode, got, err)
				}
				bid, _ := storage.ParseID(j.ID)
				oid, _ := storage.ParseID(j.OrderID)
				var exists, terminal int
				if err = r.Queue.Store.DB.QueryRow("SELECT (SELECT count(*) FROM orders WHERE id=?),(SELECT count(*) FROM task_events WHERE job_id=? AND event IN ('completed','failed'))", oid, bid).Scan(&exists, &terminal); err != nil || exists != 1 || terminal != 0 {
					t.Fatal(exists, terminal, err)
				}
				*now = now.Add(61 * time.Second)
				recovered, err := r.Queue.Claim(context.Background(), []string{kind})
				if err != nil || recovered.ID != j.ID || recovered.LeaseToken == j.LeaseToken {
					t.Fatal(recovered, err)
				}
				if changed, err := r.Queue.Finish(context.Background(), j, "done", "stale-worker", time.Time{}); err != nil || changed {
					t.Fatal("stale canceled worker wrote", changed, err)
				}
				if changed, err := r.Queue.Finish(context.Background(), recovered, "done", "confirmed-recovered-work", time.Time{}); err != nil || !changed {
					t.Fatal(changed, err)
				}
			})
		}
	}
}

func TestExecuteReadStorageFailureDoesNotCompleteExistingTask(t *testing.T) {
	for _, kind := range []string{"payment", "close", "geo"} {
		t.Run(kind, func(t *testing.T) {
			r, j, _ := runnerReadFixture(t, kind)
			readDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "closed-read.db"))
			if err != nil {
				t.Fatal(err)
			}
			if err = readDB.Close(); err != nil {
				t.Fatal(err)
			}
			r.Orders.Store = &storage.Store{DB: readDB}
			r.Execute(context.Background(), j)
			got, err := r.Queue.Get(context.Background(), j.ID)
			if err != nil || got.Status != "running" || got.Result != "" || got.LeaseToken != j.LeaseToken {
				t.Fatalf("read storage error falsely completed task: %+v %v", got, err)
			}
			bid, _ := storage.ParseID(j.ID)
			var terminal int
			if err = r.Queue.Store.DB.QueryRow("SELECT count(*) FROM task_events WHERE job_id=? AND event IN ('completed','failed')", bid).Scan(&terminal); err != nil || terminal != 0 {
				t.Fatal(terminal, err)
			}
		})
	}
}

func TestExecuteConfirmedMissingTargetStillCompletesWithValidLease(t *testing.T) {
	for _, kind := range []string{"payment", "close", "geo"} {
		t.Run(kind, func(t *testing.T) {
			r, j, _ := runnerReadFixture(t, kind)
			db := r.Queue.Store.DB
			oid, _ := storage.ParseID(j.OrderID)
			// Normal deletes cascade the job. Keep a test-only orphan to exercise the
			// existing missing-target completion branch against a real missing row.
			if _, err := db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("DELETE FROM orders WHERE id=?", oid); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
				t.Fatal(err)
			}
			r.Execute(context.Background(), j)
			got, err := r.Queue.Get(context.Background(), j.ID)
			if err != nil || got.Status != "done" || got.Result != "target_missing" {
				t.Fatal(got, err)
			}
			bid, _ := storage.ParseID(j.ID)
			var completed int
			if err = db.QueryRow("SELECT count(*) FROM task_events WHERE job_id=? AND event='completed'", bid).Scan(&completed); err != nil || completed != 1 {
				t.Fatal(completed, err)
			}
		})
	}
}
