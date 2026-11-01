package storage

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func TestBrowserSessionUpgradeBackfillsExistingSessionsAndFlows(t *testing.T) {
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
	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatal(err)
	}
	uid, iid, first, second, bound, anonymous := NewID(), NewID(), NewID(), NewID(), NewID(), NewID()
	if _, err := db.Exec("INSERT INTO users(id,created_at) VALUES(?,1)", uid[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO identities(id,user_id,provider,issuer,subject,nickname,created_at,updated_at) VALUES(?,?,'oidc','test','legacy','Legacy',1,1)", iid[:], uid[:]); err != nil {
		t.Fatal(err)
	}
	for index, sid := range [][]byte{first[:], second[:]} {
		tokenHash := make([]byte, 32)
		tokenHash[0] = byte(index + 1)
		var revoked any
		if index == 1 {
			revoked = int64(5)
		}
		if _, err := db.Exec("INSERT INTO sessions(id,user_id,identity_id,token_hash,csrf_hash,created_at,renewed_at,expires_at,revoked_at) VALUES(?,?,?,?,?,1,1,?,?)", sid, uid[:], iid[:], tokenHash, make([]byte, 32), time.Now().Add(30*24*time.Hour).Unix(), revoked); err != nil {
			t.Fatal(err)
		}
	}
	for index, fid := range [][]byte{bound[:], anonymous[:]} {
		stateHash := make([]byte, 32)
		stateHash[0] = byte(index + 1)
		var session any
		if index == 0 {
			session = first[:]
		}
		if _, err := db.Exec("INSERT INTO auth_flows(id,state_hash,cookie_hash,provider,intent,session_id,nonce,secret,created_at,expires_at) VALUES(?,?,?,'oidc','login',?,'nonce',x'',1,1000)", fid, stateHash, make([]byte, 32), session); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var sessions, families, mapped int
	for query, target := range map[string]*int{
		"SELECT count(*) FROM sessions":         &sessions,
		"SELECT count(*) FROM browser_families": &families,
		"SELECT count(*) FROM sessions s JOIN browser_families f ON f.id=s.browser_family_id WHERE f.current_session_id=s.id AND f.current_issued_order=s.issued_order AND s.issued_order>0": &mapped,
	} {
		if err := store.DB.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if sessions != 2 || families != 2 || mapped != 2 {
		t.Fatalf("backfill sessions/families/current=%d/%d/%d", sessions, families, mapped)
	}
	var family []byte
	if err := store.DB.QueryRow("SELECT browser_family_id FROM auth_flows WHERE id=?", bound[:]).Scan(&family); err != nil || string(family) != string(first[:]) {
		t.Fatalf("existing flow lost family: %x %v", family, err)
	}
	if err := store.DB.QueryRow("SELECT browser_family_id FROM auth_flows WHERE id=?", anonymous[:]).Scan(&family); err != nil || family != nil {
		t.Fatalf("anonymous old flow prematurely assigned: %x %v", family, err)
	}
	var revoked, sequence, maxOrder int64
	if err := store.DB.QueryRow("SELECT revoked_at FROM sessions WHERE id=?", second[:]).Scan(&revoked); err != nil || revoked != 5 {
		t.Fatalf("revocation changed: %d %v", revoked, err)
	}
	if err := store.DB.QueryRow("SELECT issued_order FROM browser_session_sequence WHERE id=1").Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.QueryRow("SELECT max(issued_order) FROM sessions").Scan(&maxOrder); err != nil || sequence < maxOrder {
		t.Fatalf("issuance sequence behind migrated sessions: %d/%d %v", sequence, maxOrder, err)
	}
}
