package storage

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRestorePreservesOldestSourceDuringRetention(t *testing.T) {
	for _, siblingDate := range []string{"20000101T000000Z", "99990101T000000Z"} {
		t.Run(siblingDate, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, "donate.db")
			backups := filepath.Join(dir, "backups")
			s, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			if _, err = s.DB.Exec("INSERT INTO users(id,created_at) VALUES(zeroblob(16),11)"); err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(backups, 0700); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(backups, "donate-00000101T000000Z-source.db")
			originals := map[string]bool{source: true}
			for i := range 7 {
				snapshot := source
				if i > 0 {
					snapshot = filepath.Join(backups, fmt.Sprintf("donate-%s-%d.db", siblingDate, i))
				}
				originals[snapshot] = true
				if _, err = s.DB.Exec("VACUUM INTO ?", snapshot); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.DB.Exec("UPDATE users SET created_at=22"); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if err = Restore(ctx, source, path); err != nil {
				t.Fatal("restoring the oldest retained snapshot failed", err)
			}
			after, err := os.ReadFile(source)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("restore removed or changed its source snapshot", err)
			}
			// Windows also requires Restore to release the source's file handle.
			if err = os.Rename(source, source+".moved"); err != nil {
				t.Fatal("restore left the source open", err)
			}
			if err = os.Rename(source+".moved", source); err != nil {
				t.Fatal(err)
			}
			readCreatedAt := func(snapshot string) int64 {
				t.Helper()
				db, err := sql.Open("sqlite", snapshot)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				var created int64
				if err = db.QueryRow("SELECT created_at FROM users").Scan(&created); err != nil {
					t.Fatal(err)
				}
				return created
			}
			if got := readCreatedAt(path); got != 11 {
				t.Fatalf("restored created_at=%d, want 11", got)
			}
			paths, err := filepath.Glob(filepath.Join(backups, "donate-*.db"))
			if err != nil || len(paths) != 7 {
				t.Fatal("restore retention must keep seven snapshots", len(paths), err)
			}
			rollback := ""
			for _, snapshot := range paths {
				if !originals[snapshot] {
					rollback = snapshot
				}
			}
			if rollback == "" || readCreatedAt(rollback) != 22 {
				t.Fatal("restore did not retain the previous database snapshot")
			}
			// Protection lasts for this restore; ordinary backups still rotate normally.
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err = (&Store{DB: db}).Backup(ctx, backups); err != nil {
				t.Fatal(err)
			}
			paths, err = filepath.Glob(filepath.Join(backups, "donate-*.db"))
			if err != nil || len(paths) != 7 {
				t.Fatal("ordinary retention must still keep seven snapshots", len(paths), err)
			}
			if _, err = os.Stat(source); !os.IsNotExist(err) {
				t.Fatal("restore source remained exempt from later ordinary retention", err)
			}
		})
	}
}

func TestConsistentBackupsRetainSevenSnapshots(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, e := Open(ctx, filepath.Join(dir, "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for range 9 {
		if _, e = s.Backup(ctx, filepath.Join(dir, "snapshots")); e != nil {
			t.Fatal(e)
		}
	}
	paths, e := filepath.Glob(filepath.Join(dir, "snapshots", "donate-*.db"))
	if e != nil || len(paths) != 7 {
		t.Fatal(len(paths), e)
	}
}

func TestRestoreValidatesSnapshotAndRevokesCredentials(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "donate.db")
	s, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	for _, sql := range []string{
		"INSERT INTO users(id,created_at) VALUES(zeroblob(16),0)",
		"INSERT INTO identities(id,user_id,provider,issuer,subject,nickname,created_at,updated_at) VALUES(zeroblob(16),zeroblob(16),'oidc','test','one','User',0,0)",
		"INSERT INTO sessions(id,user_id,identity_id,token_hash,csrf_hash,created_at,renewed_at,expires_at) VALUES(zeroblob(16),zeroblob(16),zeroblob(16),randomblob(32),randomblob(32),0,0,9999999999)",
		"INSERT INTO auth_flows(id,state_hash,cookie_hash,provider,intent,nonce,secret,created_at,expires_at) VALUES(randomblob(16),randomblob(32),randomblob(32),'oidc','login','nonce',randomblob(32),0,9999999999)",
	} {
		if _, e = s.DB.Exec(sql); e != nil {
			t.Fatal(e)
		}
	}
	backup, e := s.Backup(ctx, filepath.Join(filepath.Dir(path), "snapshots"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("UPDATE identities SET nickname='Changed'"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if e = Restore(ctx, backup, path); e != nil {
		t.Fatal(e)
	}
	s, e = Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var nickname string
	if e = s.DB.QueryRow("SELECT nickname FROM identities").Scan(&nickname); e != nil || nickname != "User" {
		t.Fatal(nickname, e)
	}
	for _, table := range []string{"sessions", "auth_flows"} {
		var n int
		if e = s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatal(table, n, e)
		}
	}
}

func TestRestoreRejectsCorruptionWithoutChangingCurrentDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "donate.db")
	s, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	bad := filepath.Join(dir, "bad.db")
	if e = os.WriteFile(bad, []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = Restore(ctx, bad, path); e == nil {
		t.Fatal("corruption accepted")
	}
	after, e := os.ReadFile(path)
	if e != nil || string(before) != string(after) {
		t.Fatal("current database changed", e)
	}
}

func TestRestoreBackupFailureLeavesCurrentDatabaseUnchanged(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "donate.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err = s.DB.Exec("INSERT INTO users(id,created_at) VALUES(zeroblob(16),11)"); err != nil {
		t.Fatal(err)
	}
	source, err := s.Backup(ctx, filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE users SET created_at=22"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "backups"), []byte("blocked backup directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Restore(ctx, source, path); err == nil {
		t.Fatal("restore ignored failure to back up the current database")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed pre-restore backup changed the current database", err)
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal("failed restore removed its source", err)
	}
	staged, err := filepath.Glob(path + ".restore-*")
	if err != nil || len(staged) != 0 {
		t.Fatal("failed restore left a staged database", staged, err)
	}
}
