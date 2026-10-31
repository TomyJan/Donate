package storage

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestGeoChecksUpgradePreservesExistingSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 5); err != nil {
		t.Fatal(err)
	}
	oid, lid := NewID(), NewID()
	if _, err = db.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(?,'migration-order',1,'alipay',1,2,'1.1.1.1')", oid[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO order_locations(id,order_id,country,province,source,queried_at) VALUES(?,?,'中国','广东','mmdb',1)", lid[:], oid[:]); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var province, source string
	if err = store.DB.QueryRow("SELECT province,source FROM order_locations").Scan(&province, &source); err != nil || province != "广东" || source != "mmdb" {
		t.Fatal(province, source, err)
	}
	var count int
	if err = store.DB.QueryRow("SELECT count(*) FROM order_location_checks").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
