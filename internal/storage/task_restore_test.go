package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"
)

// Restoring an abandoned lease is a new scheduling event, not a completion
// of the old worker and not a measurable execution lasting through downtime.
func TestRestoreTaskHistoryUsesRestoreTimeAndUnknownDuration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, filepath.Join(dir, "original.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	past := time.Now().Add(-48 * time.Hour).Truncate(time.Second).Unix()
	oid, jid := NewID(), NewID()
	if _, err = s.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'restored-history',1,'alipay',?,?,'192.0.2.1')", oid[:], past, past+3600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("INSERT INTO jobs(id,kind,order_id,run_at,last_result,request_id,created_at,updated_at) VALUES(?,'payment',?,?,'unpaid','old-request',?,?)", jid[:], oid[:], past, past, past); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE jobs SET status='running',attempts=1,lease_token='old-lease',lease_until=?,updated_at=? WHERE id=?", past+60, past, jid[:]); err != nil {
		t.Fatal(err)
	}
	backup, err := s.Backup(ctx, filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "restored.db")
	before := time.Now().Unix()
	if err = Restore(ctx, backup, target); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	after := time.Now().Unix()
	var status, result, source, message, request string
	var updated, eventAt, total, attempts int64
	var duration, started sql.NullInt64
	var lease sql.NullString
	if err = restored.DB.QueryRow("SELECT status,last_result,updated_at,total_attempts,attempts,lease_token,started_at FROM jobs WHERE id=?", jid[:]).Scan(&status, &result, &updated, &total, &attempts, &lease, &started); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || result != "unpaid" || total != 1 || attempts != 1 || lease.Valid {
		t.Fatal(status, result, total, attempts, lease)
	}
	if err = restored.DB.QueryRow("SELECT created_at,duration_ms,source,message,request_id FROM task_events WHERE job_id=? ORDER BY sequence DESC LIMIT 1", jid[:]).Scan(&eventAt, &duration, &source, &message, &request); err != nil {
		t.Fatal(err)
	}
	if updated < before || updated > after || eventAt != updated {
		t.Errorf("restore now=[%d,%d], updated=%d event=%d", before, after, updated, eventAt)
	}
	if duration.Valid || started.Valid {
		t.Errorf("restore fabricated execution duration: duration=%+v started=%+v", duration, started)
	}
	if source != "restore" || request != "" || !strings.Contains(message, "恢复") {
		t.Errorf("restore event attribution: source=%q request=%q message=%q", source, request, message)
	}
	// The restore attribution belongs to this one event, not a later wakeup
	// that happens before the ready task is claimed again.
	if _, err = restored.DB.Exec("UPDATE jobs SET source='manual',request_id='new-wakeup',run_at=?,updated_at=? WHERE id=?", after+1, after+1, jid[:]); err != nil {
		t.Fatal(err)
	}
	if err = restored.DB.QueryRow("SELECT source,request_id,message FROM task_events WHERE job_id=? ORDER BY sequence DESC LIMIT 1", jid[:]).Scan(&source, &request, &message); err != nil || source != "manual" || request != "new-wakeup" || strings.Contains(message, "恢复") {
		t.Fatalf("later wakeup retained restore attribution: %q %q %q %v", source, request, message, err)
	}
}

func TestRestoreLegacyRunningTaskUpdatesTimeBeforeHistoryUpgrade(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	initial, err := migrations.ReadFile("migrations/00001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, fstest.MapFS{"00001_init.sql": &fstest.MapFile{Data: initial}}, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	oid, jid := NewID(), NewID()
	past := time.Now().Add(-48 * time.Hour).Unix()
	if _, err = db.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'legacy-restored',1,'alipay',?,?,'192.0.2.1')", oid[:], past, past+3600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO jobs(id,kind,order_id,status,attempts,run_at,last_result,lease_token,lease_until,created_at,updated_at) VALUES(?,'payment',?,'running',7,?,'unpaid','old-lease',?,?,?)", jid[:], oid[:], past, past+60, past, past); err != nil {
		t.Fatal(err)
	}
	backup, err := (&Store{DB: db}).Backup(ctx, filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "restored.db")
	before := time.Now().Unix()
	if err = Restore(ctx, backup, target); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after := time.Now().Unix()
	var status, result, event string
	var updated, attempts, total, events int64
	var lease sql.NullString
	if err = s.DB.QueryRow("SELECT status,last_result,updated_at,attempts,total_attempts,lease_token FROM jobs WHERE id=?", jid[:]).Scan(&status, &result, &updated, &attempts, &total, &lease); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || result != "unpaid" || attempts != 7 || total != 0 || lease.Valid || updated < before || updated > after {
		t.Fatal(status, result, attempts, total, lease, updated, before, after)
	}
	if err = s.DB.QueryRow("SELECT count(*),max(event) FROM task_events WHERE job_id=?", jid[:]).Scan(&events, &event); err != nil || events != 1 || event != "snapshot" {
		t.Fatal(events, event, err)
	}
}
