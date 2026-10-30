package jobs

import (
	"context"
	"database/sql"
	"errors"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

type syntheticLocator struct{ refresh func() (geo.Location, error) }

func queueSyntheticRefresh(t *testing.T, q *Queue, id, operation string) string {
	t.Helper()
	oid, _ := storage.ParseID(id)
	var jid []byte
	err := q.Store.Write(context.Background(), func(tx *sql.Tx) error {
		var err error
		jid, err = EnqueueGeoRefreshTx(context.Background(), tx, oid, nil, operation, q.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return storage.IDString(jid)
}

func TestGeoRefreshDuplicateKeepsRetryBudgetAndLease(t *testing.T) {
	_, _, q, now, id := geoRefreshSetup(t)
	jid := queueSyntheticRefresh(t, q, id, "one")
	j, err := q.Claim(context.Background(), []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	if same := queueSyntheticRefresh(t, q, id, "two"); same != jid {
		t.Fatal("duplicate created a second job", same, jid)
	}
	active, err := q.Get(context.Background(), jid)
	if err != nil || active.LeaseToken != j.LeaseToken || active.Status != "running" {
		t.Fatal(active, err)
	}
	next := now.Add(10 * time.Minute)
	if _, err = q.Finish(context.Background(), j, "ready", "source_unavailable", next); err != nil {
		t.Fatal(err)
	}
	if same := queueSyntheticRefresh(t, q, id, "three"); same != jid {
		t.Fatal(same, jid)
	}
	pending, err := q.Get(context.Background(), jid)
	if err != nil || pending.Attempts != 1 || !pending.RunAt.Equal(next) {
		t.Fatal("duplicate reset retry backoff/budget", pending, err)
	}
}

func TestGeoRefreshUpgradeWaitsForAutomaticLeaseAndResetsBudget(t *testing.T) {
	db, orders, q, _, id := geoRefreshSetup(t)
	ctx := context.Background()
	jid, err := q.Enqueue(ctx, "geo", id, "automatic", "", "old")
	if err != nil {
		t.Fatal(err)
	}
	j, err := q.Claim(ctx, []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	bid, _ := storage.ParseID(jid)
	if _, err = db.DB.Exec("UPDATE jobs SET attempts=3 WHERE id=?", bid); err != nil {
		t.Fatal(err)
	}
	if same := queueSyntheticRefresh(t, q, id, "forced"); same != jid {
		t.Fatal(same, jid)
	}
	if _, err = q.Claim(ctx, []string{"geo"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("forced work stole a live lease", err)
	}
	runner := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{}}
	runner.Execute(ctx, j)
	var province string
	if err = db.DB.QueryRow("SELECT province FROM order_locations").Scan(&province); err != nil || province != "原省" {
		t.Fatal("superseded automatic result saved", province, err)
	}
	fresh, err := q.Claim(ctx, []string{"geo"})
	if err != nil || fresh.Source != GeoRefreshSource || fresh.Attempts != 1 {
		t.Fatal("refresh did not get an independent retry budget", fresh, err)
	}
}

func TestGeoRefreshUpgradeAfterLeaseExpiryRunsOnce(t *testing.T) {
	_, orders, q, now, id := geoRefreshSetup(t)
	ctx := context.Background()
	if _, err := q.Enqueue(ctx, "geo", id, "automatic", "", "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Claim(ctx, []string{"geo"}); err != nil {
		t.Fatal(err)
	}
	queueSyntheticRefresh(t, q, id, "forced")
	*now = now.Add(time.Minute)
	fresh, err := q.Claim(ctx, []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{refresh: func() (geo.Location, error) {
		return geo.Location{Country: "中国", Province: "恢复后新省", Source: "synthetic", QueriedAt: *now}, nil
	}}}
	r.Execute(ctx, fresh)
	got, err := q.Get(ctx, fresh.ID)
	if err != nil || got.Status != "done" {
		t.Fatal("recovered refresh unnecessarily reruns", got, err)
	}
	if _, err = q.Claim(ctx, []string{"geo"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}

func (s syntheticLocator) Lookup(context.Context, string) (geo.Location, error) {
	return geo.Location{Country: "中国", Province: "缓存旧省", Source: "synthetic", QueriedAt: time.Now()}, nil
}
func (s syntheticLocator) Refresh(context.Context, string) (geo.Location, error) { return s.refresh() }

func geoRefreshSetup(t *testing.T) (*storage.Store, *order.Service, *Queue, *time.Time, string) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Now().Truncate(time.Second)
	id := storage.NewID()
	lid := storage.NewID()
	if _, err = db.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip,protocol) VALUES(?,'legacy-synthetic',1,'alipay',?,?,'1.1.1.1','legacy')", id[:], now.Unix(), now.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO order_locations(id,order_id,country,province,source,queried_at) VALUES(?,?,'中国','原省','original',?)", lid[:], id[:], now.Unix()); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := NewQueue(db, log)
	q.Now = func() time.Time { return now }
	orders := order.New(db, log, "", "", "v2")
	return db, orders, q, &now, id.String()
}

func TestGeoRefreshReplacesExistingLocationAndPreservesOnFailure(t *testing.T) {
	for _, mode := range []string{"refresh", "automatic", "failure", "empty"} {
		t.Run(mode, func(t *testing.T) {
			db, orders, q, now, id := geoRefreshSetup(t)
			source := "admin_refresh"
			if mode == "automatic" {
				source = "automatic"
			}
			if _, err := q.Enqueue(context.Background(), "geo", id, source, "", "operation"); err != nil {
				t.Fatal(err)
			}
			j, err := q.Claim(context.Background(), []string{"geo"})
			if err != nil {
				t.Fatal(err)
			}
			r := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{refresh: func() (geo.Location, error) {
				if mode == "failure" {
					return geo.Location{}, errors.New("synthetic unavailable")
				}
				if mode == "empty" {
					return geo.Location{Source: "unavailable"}, nil
				}
				lat, lng, radius := 31.2, 121.5, 10.0
				return geo.Location{Country: "中国", Province: "新省", City: "合成市", District: "合成区", Street: "合成街道", ISP: "合成网络", ASN: "AS1", Latitude: &lat, Longitude: &lng, AccuracyRadius: &radius, Source: "fresh-synthetic", Version: "synthetic-v2", QueriedAt: q.Now()}, nil
			}}}
			r.Execute(context.Background(), j)
			if mode == "failure" || mode == "empty" {
				for range 2 {
					pending, err := q.Get(context.Background(), j.ID)
					if err != nil {
						t.Fatal(err)
					}
					*now = pending.RunAt
					retry, err := q.Claim(context.Background(), []string{"geo"})
					if err != nil {
						t.Fatal(err)
					}
					r.Execute(context.Background(), retry)
				}
			}
			var province string
			if err = db.DB.QueryRow("SELECT province FROM order_locations").Scan(&province); err != nil {
				t.Fatal(err)
			}
			want := "原省"
			if mode == "refresh" {
				want = "新省"
			}
			if province != want {
				t.Fatalf("province=%s want %s", province, want)
			}
			result, err := q.Get(context.Background(), j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (mode == "failure" || mode == "empty") && result.Status == "done" {
				t.Fatal("failed refresh falsely succeeded", result)
			}
			if (mode == "failure" || mode == "empty") && (result.Status != "failed" || result.Attempts != 3 || result.Result != "unknown") {
				t.Fatal("refresh retry budget", result)
			}
			if mode == "refresh" {
				var city, district, street, isp, asn, source, version string
				var lat, lng, radius float64
				var at int64
				if err = db.DB.QueryRow("SELECT city,district,street,isp,asn,latitude,longitude,accuracy_radius,source,source_version,queried_at FROM order_locations").Scan(&city, &district, &street, &isp, &asn, &lat, &lng, &radius, &source, &version, &at); err != nil {
					t.Fatal(err)
				}
				if city != "合成市" || district != "合成区" || street != "合成街道" || isp != "合成网络" || asn != "AS1" || lat != 31.2 || lng != 121.5 || radius != 10 || source != "fresh-synthetic" || version != "synthetic-v2" || at != now.Unix() {
					t.Fatal("incomplete refreshed snapshot", city, district, street, isp, asn, lat, lng, radius, source, version, at)
				}
			}
			o, err := orders.Get(context.Background(), id)
			if err != nil || o.Status != "pending" {
				t.Fatal(o, err)
			}
		})
	}
}

func TestGeoRefreshRejectsDeletedAndExpiredLeaseWrites(t *testing.T) {
	for _, mode := range []string{"deleted", "expired", "reclaimed"} {
		t.Run(mode, func(t *testing.T) {
			db, orders, q, now, id := geoRefreshSetup(t)
			q.Enqueue(context.Background(), "geo", id, "admin_refresh", "", "operation")
			j, err := q.Claim(context.Background(), []string{"geo"})
			if err != nil {
				t.Fatal(err)
			}
			var newer Job
			r := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{refresh: func() (geo.Location, error) {
				if mode == "deleted" {
					if err := orders.Delete(context.Background(), id, ""); err != nil {
						t.Fatal(err)
					}
				} else {
					*now = now.Add(time.Minute)
					if mode == "reclaimed" {
						newer, err = q.Claim(context.Background(), []string{"geo"})
						if err != nil {
							t.Fatal(err)
						}
						replacement := Runner{Queue: q, Orders: orders, Geo: syntheticLocator{refresh: func() (geo.Location, error) {
							return geo.Location{Country: "中国", Province: "新租约结果", Source: "new-worker", QueriedAt: *now}, nil
						}}}
						replacement.Execute(context.Background(), newer)
					}
				}
				return geo.Location{Country: "中国", Province: "过期结果", Source: "synthetic", QueriedAt: *now}, nil
			}}}
			r.Execute(context.Background(), j)
			var province string
			var count int
			db.DB.QueryRow("SELECT count(*) FROM order_locations WHERE province='过期结果'").Scan(&count)
			if count != 0 {
				t.Fatal("stale result was persisted")
			}
			if mode != "deleted" {
				want := "原省"
				if mode == "reclaimed" {
					want = "新租约结果"
				}
				if err = db.DB.QueryRow("SELECT province FROM order_locations").Scan(&province); err != nil || province != want {
					t.Fatal(province, err)
				}
			}
			if mode == "reclaimed" {
				got, err := q.Get(context.Background(), j.ID)
				if err != nil || got.Status != "done" || got.Result != "located_new-worker" {
					t.Fatal(got, err)
				}
			}
		})
	}
}

func TestGeoRefreshLegacyOrderWithoutLocationUsesRealMMDB(t *testing.T) {
	db, orders, q, _, id := geoRefreshSetup(t)
	if _, err := db.DB.Exec("DELETE FROM order_locations"); err != nil {
		t.Fatal(err)
	}
	locator, err := geo.New(config.Config{GeoMode: "local", GeoPath: "../geo/testdata/synthetic-city.mmdb"}, q.Log)
	if err != nil {
		t.Fatal(err)
	}
	defer locator.Close()
	jid := queueSyntheticRefresh(t, q, id, "legacy-refresh")
	j, err := q.Claim(context.Background(), []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Queue: q, Orders: orders, Geo: locator}
	r.Execute(context.Background(), j)
	var country, source string
	var radius float64
	if err = db.DB.QueryRow("SELECT country,source,accuracy_radius FROM order_locations").Scan(&country, &source, &radius); err != nil || country != "中国" || source != "mmdb" || radius != 10 {
		t.Fatal(country, source, radius, err)
	}
	got, err := q.Get(context.Background(), jid)
	if err != nil || got.Status != "done" {
		t.Fatal(got, err)
	}
	o, err := orders.Get(context.Background(), id)
	if err != nil || o.Protocol != "legacy" || o.Status != "pending" {
		t.Fatal(o, err)
	}
}
