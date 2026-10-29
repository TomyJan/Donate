package storage

import (
	"context"
	"path/filepath"
	"testing"
)

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
