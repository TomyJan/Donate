package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestGeoCheckPersistsConflictAndPreservesSnapshot(t *testing.T) {
	db, orders, q, now, id := geoRefreshSetup(t)
	jid := queueSyntheticRefresh(t, q, id, "conflicting-refresh")
	j, err := q.Claim(context.Background(), []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	p := geo.Location{Country: "中国", Source: "unknown", Verification: "conflict", Conflict: true, QueriedAt: *now, Evidence: []geo.Evidence{
		{Source: "xdb", Country: "中国", Province: "广东", Outcome: "complete", QueriedAt: *now},
		{Source: "ip9", Country: "中国", Province: "浙江", Outcome: "complete", QueriedAt: *now},
	}}
	r := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{refresh: func() (geo.Location, error) { return p, geo.ErrConflict }}}
	r.Execute(context.Background(), j)
	var province, raw string
	if err = db.DB.QueryRow("SELECT province FROM order_locations").Scan(&province); err != nil || province != "原省" {
		t.Fatal(province, err)
	}
	if err = db.DB.QueryRow("SELECT result_json FROM order_location_checks").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored geo.Location
	if err = json.Unmarshal([]byte(raw), &stored); err != nil || stored.Verification != "conflict" || len(stored.Evidence) != 2 {
		t.Fatal(raw, err)
	}
	job, err := q.Get(context.Background(), jid)
	var diagnostic string
	_ = db.DB.QueryRow("SELECT last_error FROM jobs").Scan(&diagnostic)
	if err != nil || job.Status != "ready" || diagnostic != "geo_source_conflict" {
		t.Fatal(job, err)
	}
	// A later successful attempt replaces both the decision and the old snapshot.
	*now = job.RunAt
	retry, err := q.Claim(context.Background(), []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	r.Geo = syntheticLocator{refresh: func() (geo.Location, error) {
		return geo.Location{Country: "中国", Province: "广东", City: "深圳", Source: "xdb", Version: "v3-test", Verification: "corroborated", QueriedAt: *now, Evidence: p.Evidence}, nil
	}}
	r.Execute(context.Background(), retry)
	if err = db.DB.QueryRow("SELECT province FROM order_locations").Scan(&province); err != nil || province != "广东" {
		t.Fatal(province, err)
	}
	if err = db.DB.QueryRow("SELECT result_json FROM order_location_checks").Scan(&raw); err != nil || json.Unmarshal([]byte(raw), &stored) != nil || stored.Verification != "corroborated" {
		t.Fatal(raw, err)
	}
}

func TestGeoCheckRejectsStaleLeaseAndCascadesWithOrder(t *testing.T) {
	db, orders, q, now, id := geoRefreshSetup(t)
	queueSyntheticRefresh(t, q, id, "stale-check")
	j, err := q.Claim(context.Background(), []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	r := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{refresh: func() (geo.Location, error) {
		return geo.Location{Country: "中国", Province: "广东", Source: "xdb", Verification: "single", QueriedAt: *now}, nil
	}}}
	r.Execute(context.Background(), j)
	var n int
	if err = db.DB.QueryRow("SELECT count(*) FROM order_location_checks").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	current, err := q.Claim(context.Background(), []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	r.Execute(context.Background(), current)
	if err = db.DB.QueryRow("SELECT count(*) FROM order_location_checks").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = db.DB.Exec("DELETE FROM orders"); err != nil {
		t.Fatal(err)
	}
	if err = db.DB.QueryRow("SELECT count(*) FROM order_location_checks").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestGeoCheckCancellationKeepsRetryBudget(t *testing.T) {
	db, orders, q, _, id := geoRefreshSetup(t)
	queueSyntheticRefresh(t, q, id, "shutdown-during-lookup")
	j, err := q.Claim(context.Background(), []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{refresh: func() (geo.Location, error) {
		cancel()
		return geo.Location{Source: "unknown", Verification: "unavailable"}, context.Canceled
	}}}
	r.Execute(ctx, j)
	got, err := q.Get(context.Background(), j.ID)
	if err != nil || got.Status != "ready" || got.Attempts != 0 || got.Result != "source_unavailable" {
		t.Fatalf("cancelled lookup exhausted retry budget: %+v %v", got, err)
	}
	var n int
	if err = db.DB.QueryRow("SELECT count(*) FROM order_location_checks").Scan(&n); err != nil || n != 1 {
		t.Fatal("cancelled query diagnostics missing", n, err)
	}
}

func TestGeoShutdownRestoresRetryBudgetAcrossRestart(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
	}{{"first-attempt", 0}, {"last-attempt", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			db, orders, q, now, id := geoRefreshSetup(t)
			jid := queueSyntheticRefresh(t, q, id, "shutdown-recovery")
			r := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{refresh: func() (geo.Location, error) {
				return geo.Location{}, geo.ErrUnavailable
			}}}
			for range tc.failures {
				j, err := q.Claim(context.Background(), []string{"geo"})
				if err != nil {
					t.Fatal(err)
				}
				r.Execute(context.Background(), j)
				next, err := q.Get(context.Background(), jid)
				if err != nil || next.Status != "ready" {
					t.Fatal(next, err)
				}
				*now = next.RunAt
			}
			j, err := q.Claim(context.Background(), []string{"geo"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r.Geo = syntheticLocator{refresh: func() (geo.Location, error) {
				cancel()
				return geo.Location{Verification: "unavailable"}, context.Canceled
			}}
			r.Execute(ctx, j)
			got, err := q.Get(context.Background(), jid)
			if err != nil || got.Status != "ready" || got.Attempts != tc.failures || got.LeaseToken != "" {
				t.Fatalf("shutdown consumed retry budget: %+v %v", got, err)
			}
			var total, failed int
			if err = db.DB.QueryRow("SELECT total_attempts,(SELECT count(*) FROM task_events WHERE event='failed') FROM jobs").Scan(&total, &failed); err != nil || total != tc.failures+1 || failed != 0 {
				t.Fatal("interruption lost actual execution history", total, failed, err)
			}
			var seq int
			var name, path string
			if err = db.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := storage.Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			q.Store, orders.Store = reopened, reopened
			*now = now.Add(2 * time.Minute)
			recovered, err := q.Claim(context.Background(), []string{"geo"})
			if err != nil || recovered.ID != jid || recovered.Attempts != tc.failures+1 {
				t.Fatal("interrupted task cannot resume after restart", recovered, err)
			}
			r.Geo = syntheticLocator{refresh: func() (geo.Location, error) {
				return geo.Location{Country: "中国", Province: "广东", Source: "xdb"}, nil
			}}
			r.Execute(context.Background(), recovered)
			got, err = q.Get(context.Background(), jid)
			if err != nil || got.Status != "done" {
				t.Fatal(got, err)
			}
			var province string
			if err = reopened.DB.QueryRow("SELECT province FROM order_locations").Scan(&province); err != nil || province != "广东" {
				t.Fatal(province, err)
			}
		})
	}
}

func TestGeoProviderErrorsStillExhaustRetryBudget(t *testing.T) {
	for _, cause := range []error{geo.ErrUnavailable, context.DeadlineExceeded, context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			_, orders, q, now, id := geoRefreshSetup(t)
			jid := queueSyntheticRefresh(t, q, id, "upstream-failure")
			r := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{refresh: func() (geo.Location, error) {
				return geo.Location{}, cause
			}}}
			for attempt := 1; attempt <= 3; attempt++ {
				j, err := q.Claim(context.Background(), []string{"geo"})
				if err != nil {
					t.Fatal(err)
				}
				r.Execute(context.Background(), j)
				got, err := q.Get(context.Background(), jid)
				want := "ready"
				if attempt == 3 {
					want = "failed"
				}
				if err != nil || got.Status != want || got.Attempts != attempt {
					t.Fatal("provider failure incorrectly refunded", got, err)
				}
				*now = got.RunAt
			}
		})
	}
}
