package storage

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestLibraryMigrationPreservesJobHistoryAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 7); err != nil {
		t.Fatal(err)
	}
	oid, jid := NewID(), NewID()
	if _, err := db.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'before-library-upgrade',1,'alipay',1,2,'1.1.1.1')", oid[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO jobs(id,kind,order_id,run_at,created_at,updated_at) VALUES(?,'payment',?,1,1,1)", jid[:], oid[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE jobs SET status='running',attempts=1,lease_token='old-lease',lease_until=100,updated_at=2 WHERE id=?", jid[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE jobs SET status='failed',lease_token=NULL,lease_until=NULL,last_result='test_failure',updated_at=3 WHERE id=?", jid[:]); err != nil {
		t.Fatal(err)
	}
	const history = "SELECT group_concat(row,'|') FROM (SELECT quote(id)||':'||sequence||':'||event||':'||status||':'||attempt||':'||message||':'||created_at AS row FROM task_events ORDER BY sequence)"
	var before string
	if err := db.QueryRow(history).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var after, status, resource string
	var attempts, total int
	if err := store.DB.QueryRow(history).Scan(&after); err != nil || after != before {
		t.Fatal("history changed", before, after, err)
	}
	if err := store.DB.QueryRow("SELECT status,attempts,total_attempts,resource FROM jobs WHERE id=?", jid[:]).Scan(&status, &attempts, &total, &resource); err != nil || status != "failed" || attempts != 1 || total != 1 || resource != "" {
		t.Fatal(status, attempts, total, resource, err)
	}
	system := NewID()
	if _, err := store.DB.Exec("INSERT INTO jobs(id,kind,resource,run_at,created_at,updated_at) VALUES(?,'geoip_update','mmdb',1,1,1)", system[:]); err != nil {
		t.Fatal(err)
	}
	orphan := NewID()
	if _, err := store.DB.Exec("INSERT INTO jobs(id,kind,run_at,created_at,updated_at) VALUES(?,'payment',1,1,1)", orphan[:]); err == nil {
		t.Fatal("order task without order accepted")
	}
	if _, err := store.DB.Exec("DELETE FROM orders WHERE id=?", oid[:]); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.DB.QueryRow("SELECT count(*) FROM jobs").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := store.DB.QueryRow("SELECT count(*) FROM task_events WHERE job_id=?", jid[:]).Scan(&count); err != nil || count != 0 {
		t.Fatal("orphaned history", count, err)
	}
	rows, err := store.DB.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
}
