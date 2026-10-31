package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
)

func TestTaskHistoryMigrationOnlySnapshotsExistingJobs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{}
	entries, _ := migrations.ReadDir("migrations")
	for _, entry := range entries {
		if entry.Name() >= "00005" {
			continue
		}
		data, e := migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		files[entry.Name()] = &fstest.MapFile{Data: data}
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	oid, jid := NewID(), NewID()
	if _, err = db.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'legacy-task',1,'alipay',1,2,'192.0.2.1')", oid[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO jobs(id,kind,order_id,status,attempts,last_result,run_at,created_at,updated_at) VALUES(?,'payment',?,'done',9,'paid',2,1,2)", jid[:], oid[:]); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var status, result, event string
	var attempts, total, recording, n, at int64
	var eid []byte
	if err = s.DB.QueryRow("SELECT status,last_result,attempts,total_attempts,recording_since FROM jobs WHERE id=?", jid[:]).Scan(&status, &result, &attempts, &total, &recording); err != nil {
		t.Fatal(err)
	}
	if status != "done" || result != "paid" || attempts != 9 || total != 0 || recording <= 2 {
		t.Fatal(status, result, attempts, total, recording)
	}
	if err = s.DB.QueryRow("SELECT id,event,created_at FROM task_events WHERE job_id=?", jid[:]).Scan(&eid, &event, &at); err != nil {
		t.Fatal(err)
	}
	u, err := uuid.FromBytes(eid)
	if err != nil || u.Version() != 4 || event != "snapshot" || at != recording {
		t.Fatal(u, event, at, err)
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM task_events").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
