package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/geo"
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
	if err != nil || got.Status != "ready" || got.Attempts != 1 || got.Result != "source_unavailable" {
		t.Fatalf("cancelled lookup exhausted retry budget: %+v %v", got, err)
	}
	var n int
	if err = db.DB.QueryRow("SELECT count(*) FROM order_location_checks").Scan(&n); err != nil || n != 1 {
		t.Fatal("cancelled query diagnostics missing", n, err)
	}
}
