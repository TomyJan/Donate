package storage

import (
	"context"
	"github.com/pressly/goose/v3"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFailedMigrationRollsBackItsSchemaChanges(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	initial, e := migrations.ReadFile("migrations/00001_init.sql")
	if e != nil {
		t.Fatal(e)
	}
	fs := fstest.MapFS{"00001_init.sql": {Data: initial}, "00002_failure.sql": {Data: []byte("-- +goose Up\nCREATE TABLE should_rollback(id INTEGER);\nINSERT INTO intentional_missing VALUES(1);\n-- +goose Down\nDROP TABLE should_rollback;\n")}}
	provider, e := goose.NewProvider(goose.DialectSQLite3, s.DB, fs, goose.WithDisableGlobalRegistry(true))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = provider.Up(ctx); e == nil || !strings.Contains(e.Error(), "intentional_missing") {
		t.Fatal("wrong migration failure", e)
	}
	var n int
	if e = s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='should_rollback'").Scan(&n); e != nil || n != 0 {
		t.Fatal("failed schema survived", n, e)
	}
}

func TestMigrationsAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "donate.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM users").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("INSERT INTO users(id,status,created_at) VALUES(randomblob(16),'invalid',0)"); err == nil {
		t.Fatal("invalid status accepted")
	}
	s.Close()
	s, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.DB.Exec("INSERT INTO orders(id,user_id,merchant_order_no,amount_fen,channel,created_at,expires_at,request_ip) VALUES(randomblob(16),randomblob(16),'missing-owner',1,'alipay',0,1,'127.0.0.1')"); err == nil {
		t.Fatal("missing owner accepted")
	}
}

func TestRegressionDatabasePathRetainsURLReservedCharacters(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, component := range []string{"store#one", "store%20one", "数据 空格"} {
		t.Run(component, func(t *testing.T) {
			path := filepath.Join(component, "donate.db")
			absolute, e := filepath.Abs(path)
			if e != nil {
				t.Fatal(e)
			}
			s, e := Open(context.Background(), path)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			var seq int
			var name, actual string
			if e = s.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &actual); e != nil {
				t.Fatal(e)
			}
			t.Logf("expected=%s actual=%s", path, actual)
			if filepath.Clean(actual) != filepath.Clean(absolute) {
				t.Fatal("opened a different database path")
			}
			if _, e = s.DB.Exec("INSERT INTO users(id,created_at) VALUES(randomblob(16),0)"); e != nil {
				t.Fatal(e)
			}
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			reopened, e := Open(context.Background(), absolute)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			var count int
			if e = reopened.DB.QueryRow("SELECT count(*) FROM users").Scan(&count); e != nil || count != 1 {
				t.Fatal("database contents did not survive reopening and backup", count, e)
			}
		})
	}
}
