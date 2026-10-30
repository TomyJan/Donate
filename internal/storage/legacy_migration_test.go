package storage

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestLegacyMigrationPreservesOrderChildrenAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 2); err != nil {
		t.Fatal(err)
	}
	oid, jid, lid, pid := NewID(), NewID(), NewID(), NewID()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'existing',1,'alipay',1,2,'192.0.2.1')", []any{oid[:]}},
		{"INSERT INTO jobs(id,kind,order_id,run_at,created_at,updated_at) VALUES(?,'payment',?,1,1,1)", []any{jid[:], oid[:]}},
		{"INSERT INTO order_locations(id,order_id,source,queried_at) VALUES(?,?,'test',1)", []any{lid[:], oid[:]}},
		{"INSERT INTO payment_observations(id,order_id,source,request_id,trade_no,status,amount_fen,observed_at,result) VALUES(?,?,'test','request','trade','paid',1,1,'paid')", []any{pid[:], oid[:]}},
	} {
		if _, err = db.Exec(statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, table := range []string{"orders", "jobs", "order_locations", "payment_observations"} {
		var count int
		if err = store.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("migration lost %s: count=%d error=%v", table, count, err)
		}
	}
	legacy := NewID()
	_, err = store.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,status,created_at,expires_at,paid_at,protocol,request_ip,donor_qq_number) VALUES(?,'historic',1,'qqpay','paid',1,2,1,'legacy','','123456')", legacy[:])
	if err != nil {
		t.Fatal("historical QQ payments without a saved platform transaction must remain representable", err)
	}
	invalid := NewID()
	if _, err = store.DB.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,status,created_at,expires_at,paid_at,request_ip) VALUES(?,'invalid',1,'alipay','paid',1,2,1,'')", invalid[:]); err == nil {
		t.Fatal("live payments must still require verified transaction numbers")
	}
	if _, err = store.DB.Exec("DELETE FROM orders WHERE id=?", oid[:]); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = store.DB.QueryRow("SELECT count(*) FROM jobs").Scan(&count); err != nil || count != 0 {
		t.Fatal("foreign-key cascades were not restored", count, err)
	}
}
