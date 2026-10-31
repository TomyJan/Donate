package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/jobs"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func taskFixture(t *testing.T) (*Service, *jobs.Queue, string, time.Time) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(db, config.Config{}, l)
	c, err := a.LoginIdentity(ctx, auth.Identity{Provider: "oidc", Issuer: "tasks", Subject: "admin", Nickname: "管理员"})
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, order.New(db, l, "", "", "v2"), config.Config{AdminIDs: map[string]bool{c.Session.UserID: true}, GeoMode: "online"}, l)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	q := jobs.NewQueue(db, l)
	q.Now = s.Now
	return s, q, c.Session.UserID, now
}
func taskOrder(t *testing.T, s *Service, q *jobs.Queue, kind, source, nickname string) (string, string) {
	t.Helper()
	id := storage.NewID()
	now := s.Now()
	_, err := s.Store.DB.Exec("INSERT INTO orders(id,merchant_order_no,donor_nickname,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,?,?,1,'alipay',?,?,'192.0.2.1')", id[:], "task-"+id.String(), nickname, now.Unix(), now.Add(time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	jid, err := q.Enqueue(context.Background(), kind, id.String(), source, "", "created-request")
	if err != nil {
		t.Fatal(err)
	}
	return jid, id.String()
}
func taskState(t *testing.T, s *Service, id, status string, run int64) {
	t.Helper()
	bid, _ := storage.ParseID(id)
	_, err := s.Store.DB.Exec("UPDATE jobs SET status=?,run_at=?,attempts=5,updated_at=? WHERE id=?", status, run, s.Now().Unix(), bid)
	if err != nil {
		t.Fatal(err)
	}
}

// An expedite counted as a normal reschedule would hide administrator retries
// from the execution trend and timeline even though the queue was mutated.
func TestTaskOverviewCountsReadyExpediteAsRetried(t *testing.T) {
	s, q, actor, now := taskFixture(t)
	id, _ := taskOrder(t, s, q, "geo", "admin_refresh", "属地")
	taskState(t, s, id, "ready", now.Add(time.Hour).Unix())
	out, err := s.RetryTasks(context.Background(), actor, []string{id}, "expedite", "retry-request")
	if err != nil || out.Queued != 1 {
		t.Fatal(out, err)
	}
	detail, err := s.Task(context.Background(), id)
	if err != nil || detail.Source != "admin_refresh" || detail.Attempts != 5 || !detail.RunAt.Equal(now) {
		t.Fatal(detail, err)
	}
	overview, err := s.TaskOverviewRange(context.Background(), "7d", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Trend) != 7 || overview.Trend[6].Retried != 1 || overview.Trend[6].Created != 1 {
		t.Fatal(overview.Trend)
	}
}

func TestTaskListFiltersAndStablePagination(t *testing.T) {
	s, q, _, now := taskFixture(t)
	ctx := context.Background()
	ids := []string{}
	for i, status := range []string{"ready", "running", "done", "failed"} {
		kind := []string{"payment", "geo", "close", "geo"}[i]
		source := []string{"automatic", "admin_refresh", "admin", "manual"}[i]
		id, _ := taskOrder(t, s, q, kind, source, fmt.Sprintf("昵称%d%%", i))
		taskState(t, s, id, status, now.Unix())
		ids = append(ids, id)
	}
	for _, status := range []string{"ready", "running", "done", "failed"} {
		v, err := s.Tasks(ctx, TaskFilter{Status: status})
		if err != nil || v.Total != 1 || len(v.Items) != 1 || v.Items[0].Status != status {
			t.Fatal(status, v, err)
		}
	}
	for _, tc := range []struct {
		f TaskFilter
		n int
	}{{TaskFilter{Kind: "payment"}, 1}, {TaskFilter{Kind: "geo"}, 2}, {TaskFilter{Kind: "close"}, 1}, {TaskFilter{Source: "admin_refresh"}, 1}, {TaskFilter{Search: "昵称0%"}, 1}, {TaskFilter{Search: ids[2]}, 1}, {TaskFilter{Search: "%"}, 4}, {TaskFilter{Search: "absent"}, 0}, {TaskFilter{From: now.Add(time.Second).Format(time.RFC3339)}, 0}, {TaskFilter{To: now.Format(time.RFC3339)}, 0}} {
		v, err := s.Tasks(ctx, tc.f)
		if err != nil || v.Total != tc.n {
			t.Fatal(tc.f, v, err)
		}
	}
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	for page := 1; page <= 4; page++ {
		v, err := s.Tasks(ctx, TaskFilter{Page: page, Size: 1, Sort: "created_at", Direction: "asc"})
		if err != nil || v.Total != 4 || len(v.Items) != 1 || v.Items[0].ID != sorted[page-1] {
			t.Fatal(page, v, err)
		}
	}
	for _, f := range []TaskFilter{{Status: "bad"}, {Kind: "bad"}, {Source: "bad"}, {Sort: "bad"}, {Direction: "bad"}, {From: "bad"}, {From: now.Format(time.RFC3339), To: now.Format(time.RFC3339)}, {Size: 101}, {Page: -1}, {Search: strings.Repeat("a", 129)}} {
		if _, err := s.Tasks(ctx, f); !errors.Is(err, auth.ErrInvalid) {
			t.Fatal(f, err)
		}
	}
}

func TestTaskRetryMixedStatesConflictsAndPermissionRecheck(t *testing.T) {
	s, q, actor, now := taskFixture(t)
	ctx := context.Background()
	failed, _ := taskOrder(t, s, q, "close", "admin", "失败")
	taskState(t, s, failed, "failed", now.Unix())
	ready, _ := taskOrder(t, s, q, "payment", "manual", "等待")
	running, _ := taskOrder(t, s, q, "geo", "automatic", "恢复中")
	taskState(t, s, running, "running", now.Unix())
	done, _ := taskOrder(t, s, q, "close", "admin", "完成")
	taskState(t, s, done, "done", now.Unix())
	conflict, oid := taskOrder(t, s, q, "payment", "automatic", "冲突")
	taskState(t, s, conflict, "failed", now.Unix())
	if _, err := q.Enqueue(ctx, "payment", oid, "automatic", "", "active"); err != nil {
		t.Fatal(err)
	}
	paid, paidOrder := taskOrder(t, s, q, "payment", "automatic", "已付")
	taskState(t, s, paid, "failed", now.Unix())
	po, _ := storage.ParseID(paidOrder)
	if _, err := s.Store.DB.Exec("UPDATE orders SET status='paid',paid_at=?,trade_no='paid-trade' WHERE id=?", now.Unix(), po); err != nil {
		t.Fatal(err)
	}
	missing := storage.NewID().String()
	out, err := s.RetryTasks(ctx, actor, []string{failed, ready, running, done, conflict, paid, missing}, "mixed", "mixed-request")
	if err != nil || out.Queued != 1 {
		t.Fatal(out, err)
	}
	want := []string{"queued", "already_ready", "running", "done", "active_conflict", "not_applicable", "not_found"}
	for i, v := range out.Items {
		if v.Result != want[i] {
			t.Fatal(out)
		}
	}
	d, err := s.Task(ctx, failed)
	if err != nil || d.Attempts != 0 || d.Status != "ready" {
		t.Fatal(d, err)
	}
	if _, err = s.RetryTasks(ctx, actor, []string{failed}, "mixed", "conflict"); !errors.Is(err, order.ErrConflict) {
		t.Fatal(err)
	}
	aid, _ := storage.ParseID(actor)
	if _, err = s.Store.DB.Exec("UPDATE users SET status='banned' WHERE id=?", aid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RetryTasks(ctx, actor, []string{failed, ready, running, done, conflict, paid, missing}, "mixed", "replay"); !errors.Is(err, auth.ErrBanned) {
		t.Fatal("banned actor replayed cached mutation", err)
	}
}

func TestTaskRetryConcurrentIdempotencyOnlyResetsOnce(t *testing.T) {
	s, q, actor, now := taskFixture(t)
	ctx := context.Background()
	id, _ := taskOrder(t, s, q, "close", "admin", "并发")
	taskState(t, s, id, "failed", now.Unix())
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := s.RetryTasks(ctx, actor, []string{id}, "same", "request")
			if err != nil || out.Queued != 1 {
				t.Errorf("%+v %v", out, err)
			}
		}()
	}
	wg.Wait()
	bid, _ := storage.ParseID(id)
	var requests, retries, audits int
	if err := s.Store.DB.QueryRow("SELECT (SELECT count(*) FROM task_retry_requests),(SELECT count(*) FROM task_events WHERE job_id=? AND event='retried'),(SELECT count(*) FROM audit_events WHERE action='task.retry')", bid).Scan(&requests, &retries, &audits); err != nil || requests != 1 || retries != 1 || audits != 1 {
		t.Fatal(requests, retries, audits, err)
	}
	j, err := q.Claim(ctx, []string{"close"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RetryTasks(ctx, actor, []string{id}, "same", "after-claim"); err != nil {
		t.Fatal(err)
	}
	d, err := s.Task(ctx, id)
	if err != nil || d.Attempts != 1 || d.TotalAttempts != 1 || j.LeaseToken == "" {
		t.Fatal(d, err)
	}
}

func TestTaskHistorySameSecondOrdersBySequenceAndRedacts(t *testing.T) {
	s, q, _, now := taskFixture(t)
	ctx := context.Background()
	id, _ := taskOrder(t, s, q, "geo", "automatic", "时间线")
	j, err := q.Claim(ctx, []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = q.Finish(ctx, j, "ready", "source_unavailable", now); err != nil {
		t.Fatal(err)
	}
	j, err = q.Claim(ctx, []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = q.Finish(ctx, j, "failed", "authorization Bearer private-secret", time.Time{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"failed", "started", "rescheduled", "started", "created"}
	for i := range want {
		p, err := s.TaskHistory(ctx, id, i+1, 1)
		if err != nil || p.Total != 5 || len(p.Items) != 1 || p.Items[0].Event != want[i] {
			t.Fatal(i, p, err)
		}
		if strings.Contains(p.Items[0].Result, "private-secret") || strings.Contains(p.Items[0].Message, "private-secret") {
			t.Fatal("secret exposed", p.Items)
		}
	}
	if _, err = s.TaskHistory(ctx, storage.NewID().String(), 1, 20); !errors.Is(err, order.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestTaskOverviewZeroFillsAndBoundsLongRanges(t *testing.T) {
	s, q, _, now := taskFixture(t)
	ctx := context.Background()
	v, err := s.TaskOverviewRange(ctx, "", "", "")
	if err != nil || len(v.Trend) != 7 || v.Trend[0].Date != "2026-10-25" || v.Trend[6].Date != "2026-10-31" {
		t.Fatal(v, err)
	}
	id, _ := taskOrder(t, s, q, "geo", "automatic", "统计")
	j, err := q.Claim(ctx, []string{"geo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = q.Finish(ctx, j, "done", "located", time.Time{}); err != nil {
		t.Fatal(err)
	}
	v, err = s.TaskOverviewRange(ctx, "custom", "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z")
	if err != nil || len(v.Trend) > 90 || len(v.Trend) == 0 || v.Counts.Done != 1 {
		t.Fatal(v, err)
	}
	created, started, completed := 0, 0, 0
	for _, point := range v.Trend {
		created += point.Created
		started += point.Started
		completed += point.Completed
	}
	if created != 1 || started != 1 || completed != 1 {
		t.Fatal(created, started, completed)
	}
	bid, _ := storage.ParseID(id)
	eid := storage.NewID()
	if _, err = s.Store.DB.Exec("INSERT INTO task_events(id,job_id,sequence,event,status,attempt,source,result,message,created_at) VALUES(?,?,99,'snapshot','done',0,'automatic','paid','snapshot',?)", eid[:], bid, now.Unix()); err != nil {
		t.Fatal(err)
	}
	v, err = s.TaskOverviewRange(ctx, "7d", "", "")
	if err != nil || v.Trend[6].Completed != 1 || v.Trend[6].Created != 1 {
		t.Fatal(v, err)
	}
	v, err = s.TaskOverviewRange(ctx, "custom", "2020-01-01T00:00:00Z", "2020-01-02T00:00:00Z")
	if err != nil || v.Counts.Done != 1 {
		t.Fatal(v, err)
	}
	for _, point := range v.Trend {
		if point.Created+point.Started+point.Completed+point.Failed+point.Retried != 0 {
			t.Fatal(point)
		}
	}
	for _, tc := range []struct{ name, from, to string }{{"bad", "", ""}, {"7d", "2026-10-01T00:00:00Z", ""}, {"custom", "bad", "bad"}, {"custom", "2026-10-02T00:00:00Z", "2026-10-01T00:00:00Z"}} {
		if _, err = s.TaskOverviewRange(ctx, tc.name, tc.from, tc.to); !errors.Is(err, auth.ErrInvalid) {
			t.Fatal(tc, err)
		}
	}
}

func TestTaskOverviewShanghaiCalendarAcrossHistoricalDST(t *testing.T) {
	s, _, _, _ := taskFixture(t)
	v, err := s.TaskOverviewRange(context.Background(), "custom", "1990-09-14T15:00:00Z", "1990-09-17T16:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1990-09-15", "1990-09-16", "1990-09-17"}
	if len(v.Trend) != 3 {
		t.Fatalf("Shanghai calendar bucket count: %+v", v.Trend)
	}
	for i := range want {
		if v.Trend[i].Date != want[i] {
			t.Fatal(v.Trend)
		}
	}
}
func TestTaskRetryableMatchesDisabledGeoDecision(t *testing.T) {
	s, q, actor, now := taskFixture(t)
	id, _ := taskOrder(t, s, q, "geo", "admin_refresh", "禁用")
	taskState(t, s, id, "failed", now.Unix())
	s.Config.GeoMode = "disabled"
	d, err := s.Task(context.Background(), id)
	if err != nil || d.Retryable {
		t.Fatal(d, err)
	}
	v, err := s.Tasks(context.Background(), TaskFilter{})
	if err != nil || len(v.Items) != 1 || v.Items[0].Retryable {
		t.Fatal(v, err)
	}
	r, err := s.RetryTasks(context.Background(), actor, []string{id}, "disabled", "disabled-request")
	if err != nil || r.Items[0].Result != "not_applicable" {
		t.Fatal(r, err)
	}
}

func TestTaskRetryRollsBackMutationHistoryAndKeyOnAuditFailure(t *testing.T) {
	s, q, actor, now := taskFixture(t)
	id, _ := taskOrder(t, s, q, "close", "admin", "回滚")
	taskState(t, s, id, "failed", now.Unix())
	if _, err := s.Store.DB.Exec("CREATE TRIGGER reject_task_audit BEFORE INSERT ON audit_events WHEN NEW.action='task.retry' BEGIN SELECT RAISE(ABORT,'test audit storage failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetryTasks(context.Background(), actor, []string{id}, "rollback", "rollback-request"); err == nil {
		t.Fatal("audit failure accepted")
	}
	d, err := s.Task(context.Background(), id)
	if err != nil || d.Status != "failed" || d.Attempts != 5 {
		t.Fatal(d, err)
	}
	var keys, retried int
	if err = s.Store.DB.QueryRow("SELECT (SELECT count(*) FROM task_retry_requests),(SELECT count(*) FROM task_events WHERE event='retried')").Scan(&keys, &retried); err != nil || keys != 0 || retried != 0 {
		t.Fatal(keys, retried, err)
	}
	if _, err = s.Store.DB.Exec("DROP TRIGGER reject_task_audit"); err != nil {
		t.Fatal(err)
	}
	r, err := s.RetryTasks(context.Background(), actor, []string{id}, "rollback", "retry-storage")
	if err != nil || r.Queued != 1 {
		t.Fatal(r, err)
	}
}

func TestTaskRetryIdempotencySurvivesStoreReopen(t *testing.T) {
	s, q, actor, now := taskFixture(t)
	ctx := context.Background()
	id, _ := taskOrder(t, s, q, "close", "admin", "重启")
	taskState(t, s, id, "failed", now.Unix())
	out, err := s.RetryTasks(ctx, actor, []string{id}, "persistent", "first")
	if err != nil || out.Queued != 1 {
		t.Fatal(out, err)
	}
	// A later execution has failed again. Replaying the old key after restart
	// must return the old response, not reset the new round's retry budget.
	taskState(t, s, id, "failed", now.Unix())
	var seq int
	var name, path string
	if err = s.Store.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	s.Store = reopened
	again, err := s.RetryTasks(ctx, actor, []string{id}, "persistent", "second")
	if err != nil || again.Queued != 1 || again.Items[0] != out.Items[0] {
		t.Fatal(again, err)
	}
	d, err := s.Task(ctx, id)
	if err != nil || d.Status != "failed" || d.Attempts != 5 {
		t.Fatal(d, err)
	}
}

func TestTaskRetryRejectsInvalidBatchesAndKeys(t *testing.T) {
	s, q, actor, _ := taskFixture(t)
	id, _ := taskOrder(t, s, q, "geo", "automatic", "校验")
	many := make([]string, 101)
	for i := range many {
		many[i] = id
	}
	for _, tc := range []struct {
		ids []string
		key string
	}{{nil, "one"}, {[]string{id}, ""}, {[]string{id}, " "}, {[]string{id}, strings.Repeat("x", 129)}, {[]string{id}, "line\nbreak"}, {[]string{"invalid"}, "one"}, {many, "one"}} {
		if _, err := s.RetryTasks(context.Background(), actor, tc.ids, tc.key, "invalid"); !errors.Is(err, auth.ErrInvalid) {
			t.Fatal(tc, err)
		}
	}
}
func TestTaskRetryLogIncludesOrderAndJobIDs(t *testing.T) {
	s, q, actor, now := taskFixture(t)
	id, oid := taskOrder(t, s, q, "close", "admin", "日志")
	taskState(t, s, id, "failed", now.Unix())
	var output bytes.Buffer
	s.Log = slog.New(slog.NewTextHandler(&output, nil))
	if _, err := s.RetryTasks(context.Background(), actor, []string{id}, "log", "log-request"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "订单 "+oid) || !strings.Contains(output.String(), "任务 "+id) {
		t.Fatal(output.String())
	}
}
