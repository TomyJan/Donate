package storage

import (
	"context"
	"database/sql"
	"github.com/pressly/goose/v3"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDatabaseAndJournalsArePrivateInExistingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permission bits do not model Windows ACLs")
	}
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "donate.db")
			if existing {
				previous, err := Open(context.Background(), path)
				if err != nil {
					t.Fatal(err)
				}
				defer previous.Close()
				for _, file := range []string{path, path + "-wal", path + "-shm"} {
					if err = os.Chmod(file, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			s, err := Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for _, file := range []string{path, path + "-wal", path + "-shm"} {
				info, err := os.Stat(file)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm()&0077 != 0 {
					t.Errorf("database file %s exposes permissions %04o", filepath.Base(file), info.Mode().Perm())
				}
			}
			info, err := os.Stat(dir)
			if err != nil || info.Mode().Perm() != 0755 {
				t.Fatal("opening the database changed the existing directory's permissions")
			}
		})
	}
}

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
	identifiers, e := migrations.ReadFile("migrations/00002_qq_identifiers.sql")
	if e != nil {
		t.Fatal(e)
	}
	legacy, e := migrations.ReadFile("migrations/00003_legacy_donors.sql")
	if e != nil {
		t.Fatal(e)
	}
	fs := fstest.MapFS{"00001_init.sql": {Data: initial}, "00002_qq_identifiers.sql": {Data: identifiers}, "00003_legacy_donors.sql": {Data: legacy}, "00004_failure.sql": {Data: []byte("-- +goose Up\nCREATE TABLE should_rollback(id INTEGER);\nINSERT INTO intentional_missing VALUES(1);\n-- +goose Down\nDROP TABLE should_rollback;\n")}}
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

func TestQQIdentifierMigrationPreservesLegacyBinding(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	initial, err := migrations.ReadFile("migrations/00001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fstest.MapFS{"00001_init.sql": {Data: initial}}, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	uid, iid := NewID(), NewID()
	if _, err = db.Exec("INSERT INTO users(id,created_at) VALUES(?,1)", uid[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO identities(id,user_id,provider,issuer,subject,nickname,qq_number,created_at,updated_at) VALUES(?,?,'qq','app','legacy-open','old','123456',1,1)", iid[:], uid[:]); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(filepath.Dir(path), "legacy-snapshot.db")
	if _, err = db.Exec("VACUUM INTO ?", snapshot); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var actualID []byte
	var open, union, subject string
	if err = upgraded.DB.QueryRow("SELECT id,qq_openid,qq_unionid,subject FROM identities").Scan(&actualID, &open, &union, &subject); err != nil {
		t.Fatal(err)
	}
	if IDString(actualID) != iid.String() || open != "legacy-open" || union != "" || subject != "legacy-open" {
		t.Fatal("legacy binding changed", IDString(actualID), open, union, subject)
	}
	restoredPath := filepath.Join(filepath.Dir(path), "restored.db")
	if err = Restore(ctx, snapshot, restoredPath); err != nil {
		t.Fatal("legacy backup rejected", err)
	}
	restored, err := Open(ctx, restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.DB.QueryRow("SELECT id,qq_openid,qq_unionid,subject FROM identities").Scan(&actualID, &open, &union, &subject); err != nil {
		t.Fatal(err)
	}
	if IDString(actualID) != iid.String() || open != "legacy-open" || union != "" {
		t.Fatal("restored legacy binding changed")
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
