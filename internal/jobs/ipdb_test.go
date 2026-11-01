package jobs

import (
	"context"
	"errors"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/geo"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/storage"
)

func TestLibraryJobPersistsAndRunsWithoutOrder(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id := storage.NewID()
	now := time.Now().Unix()
	_, err = db.DB.Exec("INSERT INTO jobs(id,kind,resource,run_at,created_at,updated_at) VALUES(?,'geoip_update','xdb4',?,?,?)", id[:], now, now, now)
	if err != nil {
		t.Fatalf("system database task must not need a donor order: %v", err)
	}
	q := NewQueue(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	job, err := q.Claim(ctx, []string{"geoip_update"})
	if err != nil || job.ID != id.String() || job.OrderID != "" {
		t.Fatal(job, err)
	}
	if ok, err := q.Finish(ctx, job, "done", "database_updated", time.Time{}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	var events int
	if err := db.DB.QueryRow("SELECT count(*) FROM task_events WHERE job_id=? AND event IN ('created','started','completed')", id[:]).Scan(&events); err != nil || events != 3 {
		t.Fatalf("library task history missing: %d %v", events, err)
	}
}

// A broken downloader must remain a durable, retryable task without reading an order.
type libraryUpdater struct {
	fail  error
	calls int
}

func (u *libraryUpdater) Due(time.Time) []string { return []string{"xdb4"} }
func (u *libraryUpdater) Update(ctx context.Context, kind string) error {
	if kind != "xdb4" {
		return errors.New("unexpected database")
	}
	u.calls++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return u.fail
}
func TestLibraryTasksDeduplicateAndPreserveRetryBackoff(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().Truncate(time.Second)
	q := NewQueue(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	q.Now = func() time.Time { return now }
	updater := &libraryUpdater{fail: errors.New("download unavailable")}
	runner := &Runner{Queue: q, IPDB: updater}
	if err := runner.ScheduleIPDB(ctx); err != nil {
		t.Fatal(err)
	}
	j, err := q.Claim(ctx, []string{"geoip_update"})
	if err != nil {
		t.Fatal(err)
	}
	if j.Resource != "xdb4" || j.OrderID != "" {
		t.Fatal(j)
	}
	var lease int64
	if err := db.DB.QueryRow("SELECT lease_until FROM jobs WHERE kind='geoip_update'").Scan(&lease); err != nil || lease-now.Unix() < 180 {
		t.Fatal(lease, err)
	}
	runner.Execute(ctx, j)
	failed, err := q.Get(ctx, j.ID)
	if err != nil || failed.Status != "ready" || failed.Result != "database_update_failed" || !failed.RunAt.After(now) {
		t.Fatal(failed, err)
	}
	if err := runner.ScheduleIPDB(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := q.Get(ctx, j.ID)
	if err != nil || !after.RunAt.Equal(failed.RunAt) {
		t.Fatal("scheduler reset retry backoff", after, err)
	}
	now = failed.RunAt.Add(time.Second)
	updater.fail = nil
	j, err = q.Claim(ctx, []string{"geoip_update"})
	if err != nil {
		t.Fatal(err)
	}
	runner.Execute(ctx, j)
	done, err := q.Get(ctx, j.ID)
	if err != nil || done.Status != "done" || done.Result != "database_updated" || updater.calls != 2 {
		t.Fatal(done, err, updater.calls)
	}
	var orders, events int
	if err := db.DB.QueryRow("SELECT count(*) FROM orders").Scan(&orders); err != nil || orders != 0 {
		t.Fatal(orders, err)
	}
	if err := db.DB.QueryRow("SELECT count(*) FROM task_events WHERE job_id=?", mustJobID(j.ID)).Scan(&events); err != nil || events < 5 {
		t.Fatal(events, err)
	}
}
func mustJobID(id string) []byte { b, _ := storage.ParseID(id); return b }
func TestInterruptedLibraryTaskRefundsAttempt(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := NewQueue(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := &Runner{Queue: q, IPDB: &libraryUpdater{}}
	if err := r.ScheduleIPDB(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, err := q.Claim(context.Background(), []string{"geoip_update"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Execute(ctx, j)
	out, err := q.Get(context.Background(), j.ID)
	if err != nil || out.Status != "ready" || out.Attempts != 0 || !strings.Contains(out.Result, "interrupted") {
		t.Fatal(out, err)
	}
}

func TestQueuedLibraryDownloadRepairsMissingAndCorruptFile(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "corrupt"}[corrupt], func(t *testing.T) {
			ctx := context.Background()
			data, err := os.ReadFile("../geo/testdata/synthetic-city.mmdb")
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++; _, _ = w.Write(data) }))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "City.mmdb")
			if corrupt {
				if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			c := config.Config{Development: true, GeoMode: "local", GeoPath: path, GeoMMDBURL: server.URL, GeoUpdateDays: 7}
			locator, err := geo.New(c, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			defer locator.Close()
			db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "tasks.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			q := NewQueue(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
			r := &Runner{Queue: q, IPDB: geo.NewUpdater(locator, c, q.Log)}
			if err := r.ScheduleIPDB(ctx); err != nil {
				t.Fatal(err)
			}
			if requests != 0 {
				t.Fatal("scheduler downloaded synchronously", requests)
			}
			j, err := q.Claim(ctx, []string{"geoip_update"})
			if err != nil {
				t.Fatal(err)
			}
			r.Execute(ctx, j)
			out, err := q.Get(ctx, j.ID)
			if err != nil || out.Status != "done" || out.Result != "database_updated" {
				t.Fatal(out, err)
			}
			if p, err := locator.Lookup(ctx, "223.5.5.5"); err != nil || p.Source != "mmdb" {
				t.Fatal(p, err)
			}
			if err := r.ScheduleIPDB(ctx); err != nil {
				t.Fatal(err)
			}
			var total int
			if err := db.DB.QueryRow("SELECT count(*) FROM jobs").Scan(&total); err != nil || total != 1 || requests != 1 {
				t.Fatal(total, requests, err)
			}
		})
	}
}

func TestLibraryFailureHistoryIncludesSafeHTTPReason(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "city.mmdb")
	c := config.Config{Development: true, GeoMode: "local", GeoPath: path, GeoMMDBURL: server.URL + "?key=never-record-me", GeoUpdateDays: 7}
	locator, err := geo.New(c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer locator.Close()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := NewQueue(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := &Runner{Queue: q, IPDB: geo.NewUpdater(locator, c, q.Log)}
	if err := r.ScheduleIPDB(ctx); err != nil {
		t.Fatal(err)
	}
	j, err := q.Claim(ctx, []string{"geoip_update"})
	if err != nil {
		t.Fatal(err)
	}
	r.Execute(ctx, j)
	var reason string
	if err := db.DB.QueryRow("SELECT last_error FROM jobs WHERE id=?", mustJobID(j.ID)).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "503") || strings.Contains(reason, "never-record-me") || strings.Contains(reason, server.URL) {
		t.Fatal("download reason missing or unsafe", reason)
	}
}
