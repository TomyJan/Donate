package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/storage"
)

func taskGeoIP(t *testing.T, s *Service, resource, status string) string {
	t.Helper()
	id := storage.NewID()
	now := s.Now().Unix()
	if _, err := s.Store.DB.Exec("INSERT INTO jobs(id,kind,resource,order_id,status,attempts,run_at,source,created_at,updated_at) VALUES(?,'geoip_update',?,NULL,?,5,?,'automatic',?,?)", id[:], resource, status, now, now, now); err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func configureTaskGeoIP(s *Service) {
	s.Config.GeoMode = "local"
	s.Config.GeoUpdateDays = 7
	s.Config.GeoXDBv4, s.Config.GeoXDBv4URL = "ipv4.xdb", "https://example.com/private-ipv4.xdb"
	s.Config.GeoXDBv6, s.Config.GeoXDBv6URL = "ipv6.xdb", "https://example.com/private-ipv6.xdb"
	s.Config.GeoPath, s.Config.GeoMMDBURL = "city.mmdb", "https://example.com/private-city.mmdb"
}

func TestTaskGeoIPKindFilter(t *testing.T) {
	where, args, err := taskWhere(TaskFilter{Kind: "geoip_update"})
	if err != nil || where != "1=1 AND j.kind=?" || len(args) != 1 || args[0] != "geoip_update" {
		t.Fatal(where, args, err)
	}
}

// Dropping orderless jobs from either join would hide system updates from the
// same task list/detail/history used for order jobs.
func TestTaskGeoIPListDetailHistoryAndOverview(t *testing.T) {
	s, _, _, _ := taskFixture(t)
	configureTaskGeoIP(s)
	ctx := context.Background()
	for _, resource := range []string{"xdb4", "xdb6", "mmdb"} {
		id := taskGeoIP(t, s, resource, "failed")
		detail, err := s.Task(ctx, id)
		if err != nil || detail.OrderID != "" || detail.Nickname != "" || detail.MerchantOrderNo != "" || !detail.Retryable {
			t.Fatalf("detail=%+v err=%v", detail, err)
		}
		raw, _ := json.Marshal(detail)
		var payload map[string]any
		if err = json.Unmarshal(raw, &payload); err != nil || payload["resource"] != resource {
			t.Fatalf("payload=%s err=%v", raw, err)
		}
		history, err := s.TaskHistory(ctx, id, 1, 20)
		if err != nil || history.Total != 1 || history.Items[0].Event != "created" {
			t.Fatal(history, err)
		}
	}
	list, err := s.Tasks(ctx, TaskFilter{Kind: "geoip_update"})
	if err != nil || list.Total != 3 || len(list.Items) != 3 {
		t.Fatal(list, err)
	}
	search, err := s.Tasks(ctx, TaskFilter{Search: "xdb4"})
	if err != nil || search.Total != 1 {
		t.Fatal(search, err)
	}
	overview, err := s.TaskOverviewRange(ctx, "7d", "", "")
	if err != nil || overview.Total != 3 || overview.Counts.Failed != 3 || overview.Trend[6].Created != 3 {
		t.Fatal(overview, err)
	}
}

// Retrying must reuse the persisted resource task, reset its execution budget,
// record admin attribution, and audit the task rather than a missing order.
func TestTaskGeoIPRetryPersistsHistoryAndAudit(t *testing.T) {
	s, _, actor, now := taskFixture(t)
	configureTaskGeoIP(s)
	id := taskGeoIP(t, s, "xdb4", "failed")
	ctx := context.Background()
	for range 2 {
		out, err := s.RetryTasks(ctx, actor, []string{id}, "geoip-retry", "geoip-request")
		if err != nil || out.Queued != 1 || out.Items[0].Result != "queued" {
			t.Fatal(out, err)
		}
	}
	detail, err := s.Task(ctx, id)
	if err != nil || detail.Status != "ready" || detail.Attempts != 0 || detail.Source != "admin_retry" || detail.ActorID != actor || !detail.RunAt.Equal(now) {
		t.Fatal(detail, err)
	}
	history, err := s.TaskHistory(ctx, id, 1, 20)
	if err != nil || history.Total != 2 || history.Items[0].Event != "retried" || history.Items[0].Source != "admin_retry" {
		t.Fatal(history, err)
	}
	bid, _ := storage.ParseID(id)
	var audits int
	if err = s.Store.DB.QueryRow("SELECT count(*) FROM audit_events WHERE action='task.retry' AND target_type='task' AND target_id=?", bid).Scan(&audits); err != nil || audits != 1 {
		t.Fatal(audits, err)
	}
	audit, err := s.Audit(ctx, Filter{})
	if err != nil || len(audit.Items) != 1 || !audit.Items[0].TargetExists || audit.Items[0].Target != "xdb4" {
		t.Fatal(audit, err)
	}
	raw, _ := json.Marshal(audit.Items[0])
	var payload map[string]any
	if err = json.Unmarshal(raw, &payload); err != nil || payload["target_resource"] != "xdb4" {
		t.Fatalf("audit=%s err=%v", raw, err)
	}
	if _, err = s.Store.DB.Exec("DELETE FROM jobs WHERE id=?", bid); err != nil {
		t.Fatal(err)
	}
	audit, err = s.Audit(ctx, Filter{})
	if err != nil || len(audit.Items) != 1 || audit.Items[0].TargetExists || audit.Items[0].Target != "" {
		t.Fatal(audit, err)
	}
}

func TestTaskGeoIPRetryRequiresConfiguredUpdates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Service)
	}{
		{"online", func(s *Service) { s.Config.GeoMode = "online" }},
		{"disabled", func(s *Service) { s.Config.GeoMode = "disabled" }},
		{"zero interval", func(s *Service) { s.Config.GeoUpdateDays = 0 }},
		{"negative interval", func(s *Service) { s.Config.GeoUpdateDays = -1 }},
		{"missing path", func(s *Service) { s.Config.GeoXDBv4 = "" }},
		{"missing URL", func(s *Service) { s.Config.GeoXDBv4URL = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, actor, _ := taskFixture(t)
			configureTaskGeoIP(s)
			id := taskGeoIP(t, s, "xdb4", "failed")
			tc.change(s)
			detail, err := s.Task(context.Background(), id)
			if err != nil || detail.Retryable {
				t.Fatal(detail, err)
			}
			list, err := s.Tasks(context.Background(), TaskFilter{})
			if err != nil || len(list.Items) != 1 || list.Items[0].Retryable {
				t.Fatal(list, err)
			}
			out, err := s.RetryTasks(context.Background(), actor, []string{id}, "disabled-update", "retry-request")
			if err != nil || out.Queued != 0 || out.Items[0].Result != "not_applicable" {
				t.Fatal(out, err)
			}
		})
	}
}

func TestTaskGeoIPRetryActiveConflictUsesResource(t *testing.T) {
	s, _, actor, now := taskFixture(t)
	configureTaskGeoIP(s)
	id := taskGeoIP(t, s, "xdb4", "failed")
	_ = taskGeoIP(t, s, "xdb4", "ready")
	other := taskGeoIP(t, s, "xdb6", "failed")
	taskState(t, s, other, "ready", now.Add(time.Hour).Unix())
	out, err := s.RetryTasks(context.Background(), actor, []string{id, other}, "resource-conflict", "retry-request")
	if err != nil || out.Queued != 1 || out.Items[0].Result != "active_conflict" || out.Items[1].Result != "queued" {
		t.Fatal(out, err)
	}
}
