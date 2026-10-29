package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
