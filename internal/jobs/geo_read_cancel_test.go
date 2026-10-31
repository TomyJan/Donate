package jobs

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/storage"
)

type unavailableReadLocator struct{ calls int }

func (l *unavailableReadLocator) Lookup(context.Context, string) (geo.Location, error) {
	l.calls++
	return geo.Location{}, geo.ErrUnavailable
}
func (l *unavailableReadLocator) Refresh(ctx context.Context, ip string) (geo.Location, error) {
	return l.Lookup(ctx, ip)
}

func TestGeoReadCancellationPreservesBudgetAcrossRestart(t *testing.T) {
	for _, source := range []string{"automatic", GeoRefreshSource} {
		for _, failures := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/prior-failures-%d", source, failures), func(t *testing.T) {
				ctx := context.Background()
				db, orders, q, now, id := geoRefreshSetup(t)
				var jid string
				if source == GeoRefreshSource {
					jid = queueSyntheticRefresh(t, q, id, "read-cancellation")
				} else {
					var err error
					jid, err = q.Enqueue(ctx, "geo", id, source, "", "read-cancellation")
					if err != nil {
						t.Fatal(err)
					}
				}
				locator := &unavailableReadLocator{}
				r := Runner{Queue: q, Orders: orders, Geo: locator}
				for range failures {
					j, err := q.Claim(ctx, []string{"geo"})
					if err != nil {
						t.Fatal(err)
					}
					r.Execute(ctx, j)
					got, err := q.Get(ctx, jid)
					if err != nil || got.Status != "ready" {
						t.Fatal(got, err)
					}
					*now = got.RunAt
				}
				for interruption := 0; interruption < 3; interruption++ {
					j, err := q.Claim(ctx, []string{"geo"})
					if err != nil {
						t.Fatal(err)
					}
					canceled, cancel := context.WithCancel(ctx)
					cancel()
					r.Execute(canceled, j)
					got, err := q.Get(ctx, jid)
					if err != nil || got.Status != "ready" || got.Attempts != failures || got.LeaseToken != "" || got.RunAt.After(*now) {
						t.Fatalf("canceled read consumed lookup budget or delayed recovery: %+v %v", got, err)
					}
					var total, terminal, checks int
					if err = db.DB.QueryRow("SELECT total_attempts,(SELECT count(*) FROM task_events WHERE event IN ('failed','completed')),(SELECT count(*) FROM order_location_checks) FROM jobs").Scan(&total, &terminal, &checks); err != nil {
						t.Fatal(err)
					}
					wantChecks := 0
					if failures > 0 {
						wantChecks = 1
					}
					if total != failures+interruption+1 || terminal != 0 || checks != wantChecks || locator.calls != failures {
						t.Fatalf("interruption corrupted history or issued a lookup: total=%d terminal=%d checks=%d lookups=%d", total, terminal, checks, locator.calls)
					}
					if interruption == 0 {
						var seq int
						var name, path string
						if err = db.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
							t.Fatal(err)
						}
						if err = db.Close(); err != nil {
							t.Fatal(err)
						}
						db, err = storage.Open(ctx, path)
						if err != nil {
							t.Fatal(err)
						}
						defer db.Close()
						q.Store, orders.Store = db, db
					}
				}
				for attempt := failures + 1; attempt <= 3; attempt++ {
					j, err := q.Claim(ctx, []string{"geo"})
					if err != nil {
						t.Fatal(err)
					}
					r.Execute(ctx, j)
					got, err := q.Get(ctx, jid)
					want := "ready"
					if attempt == 3 {
						want = "failed"
					}
					if err != nil || got.Status != want || got.Attempts != attempt || locator.calls != attempt {
						t.Fatal("real failures no longer have the full retry budget", got, locator.calls, err)
					}
					*now = got.RunAt
				}
				var total, failed int
				if err := db.DB.QueryRow("SELECT total_attempts,(SELECT count(*) FROM task_events WHERE event='failed') FROM jobs").Scan(&total, &failed); err != nil || total != 6 || failed != 1 {
					t.Fatal("history must record three interruptions and three actual failures", total, failed, err)
				}
			})
		}
	}
}

func TestGeoReadCancellationCannotRefundAStaleLease(t *testing.T) {
	for _, reclaimed := range []bool{false, true} {
		t.Run(fmt.Sprintf("reclaimed-%t", reclaimed), func(t *testing.T) {
			r, old, now := runnerReadFixture(t, "geo")
			*now = now.Add(61 * time.Second)
			if reclaimed {
				if _, err := r.Queue.Claim(context.Background(), []string{"geo"}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := r.Queue.Get(context.Background(), old.ID)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r.Execute(ctx, old)
			after, err := r.Queue.Get(context.Background(), old.ID)
			if err != nil || after != before {
				t.Fatal("stale worker changed the current lease or retry budget", before, after, err)
			}
		})
	}
}
