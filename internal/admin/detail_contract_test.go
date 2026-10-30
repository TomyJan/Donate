package admin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/jobs"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestDetailProvincePrivacyAndCompleteManualRequestHistory(t *testing.T) {
	s, user := readFixture(t)
	ctx := context.Background()
	o := addReadOrder(t, s, user, "detail-contract", "pending", "2026-10-30T00:00:00Z", "", "alipay", "message", 888, false)
	oid, _ := storage.ParseID(o.ID)
	lid := storage.NewID()
	if _, err := s.Store.DB.ExecContext(ctx, "INSERT INTO order_locations(id,order_id,country,province,city,street,source,queried_at) VALUES(?,?,'中国','北京','合成测试城市','不公开街道','fixture-synthetic',?)", lid[:], oid, s.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	q := jobs.NewQueue(s.Store, s.Log)
	at := s.Now()
	q.Now = func() time.Time { return at }
	jobIDs := map[string]bool{}
	for i := 0; i < 6; i++ {
		at = at.Add(time.Minute)
		key := time.Unix(at.Unix(), 0).Format(time.RFC3339)
		jobID, err := q.EnqueueOnce(ctx, o.ID, user, "manual-request", key)
		if err != nil {
			t.Fatal(err)
		}
		jobIDs[jobID] = true
		same, err := q.EnqueueOnce(ctx, o.ID, user, "manual-retry", key)
		if err != nil || same != jobID {
			t.Fatalf("idempotent enqueue=%s/%s %v", same, jobID, err)
		}
		if i == 2 {
			j, err := q.Claim(ctx, []string{"payment"})
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := q.Finish(ctx, j, "done", "verified", time.Time{}); err != nil || !ok {
				t.Fatal(ok, err)
			}
		}
	}
	if len(jobIDs) != 2 {
		t.Fatalf("expected shared jobs then a new completed-task successor, got %v", jobIDs)
	}
	// Expiry metadata must not silently truncate retained request history, and a
	// later automatic wakeup must not relabel or erase earlier manual requests.
	at = at.Add(48 * time.Hour)
	if _, err := q.Enqueue(ctx, "payment", o.ID, "browser_return", "", "automatic-wakeup"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		id := storage.NewID()
		if _, err := s.Store.DB.ExecContext(ctx, "INSERT INTO jobs(id,kind,order_id,status,run_at,source,created_at,updated_at) VALUES(?,'payment',?,'done',?,'automatic',?,?)", id[:], oid, at.Unix(), at.Add(-time.Duration(i)*time.Second).Unix(), at.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	full, err := s.Detail(ctx, o.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(full)
	var wire map[string]any
	_ = json.Unmarshal(raw, &wire)
	if wire["province"] != "北京" || wire["location"] == nil {
		t.Errorf("admin province/location=%s", raw)
	}
	history, ok := wire["reconciliations"].([]any)
	if !ok || len(history) != 6 {
		t.Fatalf("manual request history=%s", raw)
	}
	seen := map[string]bool{}
	for _, entry := range history {
		item := entry.(map[string]any)
		id, _ := item["id"].(string)
		if _, err := storage.ParseID(id); err != nil || seen[id] {
			t.Errorf("history must use unique persisted UUIDs: %+v", item)
		}
		seen[id] = true
		if !jobIDs[item["job_id"].(string)] || item["created_at"] == nil || item["status"] == nil || item["result"] == nil {
			t.Errorf("manual request=%+v", item)
		}
	}
	taskRows := wire["jobs"].([]any)
	if len(taskRows) != 7 {
		t.Errorf("all jobs must be returned without a three-entry limit: %d", len(taskRows))
	}
	sources := map[string]bool{}
	for _, row := range taskRows {
		j := row.(map[string]any)
		source, _ := j["source"].(string)
		sources[source] = true
		if j["source"] == nil || j["created_at"] == nil {
			t.Errorf("task lacks true source/time: %+v", j)
		}
	}
	if !sources["automatic"] || !sources["browser_return"] {
		t.Errorf("task sources were relabeled: %+v", sources)
	}
	public, err := s.Detail(ctx, o.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(public)
	wire = map[string]any{}
	_ = json.Unmarshal(raw, &wire)
	if wire["province"] != "北京" {
		t.Errorf("public detail missing province: %s", raw)
	}
	for _, secret := range []string{"ip", "location", "jobs", "reconciliations"} {
		if wire[secret] != nil {
			t.Errorf("public detail leaks %s: %s", secret, raw)
		}
	}
	if err := s.Orders.Delete(ctx, o.ID, user); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Detail(ctx, o.ID, true); !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("deleted detail=%v", err)
	}
}

func TestDetailWithoutLocationKeepsProvinceEmpty(t *testing.T) {
	s, user := readFixture(t)
	o := addReadOrder(t, s, user, "no-location", "pending", "2026-10-30T00:00:00Z", "", "alipay", "", 1, false)
	got, err := s.Detail(context.Background(), o.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var wire map[string]any
	_ = json.Unmarshal(raw, &wire)
	if value, ok := wire["province"].(string); !ok || value != "" {
		t.Fatalf("unknown province must be explicit empty string: %s", raw)
	}
}
