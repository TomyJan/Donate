package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestPublicOrderSortUpgradeAndDowngradePreserveDates(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
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
	if _, err = provider.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	for _, order := range []struct {
		name, protocol string
		created        int64
		paid, trade    any
	}{
		{"legacy-old", "legacy", 100, nil, nil},
		{"live-paid", "v2", 10, 200, "trade-1"},
		{"legacy-latest", "legacy", 300, nil, nil},
	} {
		id := NewID()
		if _, err = db.Exec("INSERT INTO orders(id,merchant_order_no,amount_fen,channel,status,created_at,expires_at,paid_at,protocol,trade_no,request_ip) VALUES(?,?,1,'alipay','paid',?,1000,?,?,?,'')", id[:], order.name, order.created, order.paid, order.protocol, order.trade); err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		t.Helper()
		rows, err := db.Query("SELECT merchant_order_no,COALESCE(paid_at,created_at) FROM orders WHERE status='paid' AND hidden_order=0 ORDER BY COALESCE(paid_at,created_at) DESC,id DESC")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var name string
			var date int64
			if err = rows.Scan(&name, &date); err != nil {
				t.Fatal(err)
			}
			got = append(got, fmt.Sprintf("%s:%d", name, date))
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, ",") != "legacy-latest:300,live-paid:200,legacy-old:100" {
			t.Fatalf("migration changed order data or sorting: %v", got)
		}
	}
	check()
	if _, err = provider.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	check()
	if _, err = provider.DownTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	check()
}

func TestPublicOrderPaginationAvoidsTemporarySort(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, test := range []struct {
		name, cursor string
		args         []any
	}{
		{name: "first page"},
		{name: "cursor page", cursor: " AND (COALESCE(o.paid_at,o.created_at)<? OR (COALESCE(o.paid_at,o.created_at)=? AND o.id<?))", args: []any{100, 100, make([]byte, 16)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, err := store.DB.Query("EXPLAIN QUERY PLAN SELECT o.id,COALESCE(o.paid_at,o.created_at),i.nickname,l.province FROM orders o LEFT JOIN users u ON u.id=o.user_id LEFT JOIN identities i ON i.id=u.public_identity_id LEFT JOIN order_locations l ON l.order_id=o.id WHERE o.status='paid' AND o.hidden_order=0"+test.cursor+" ORDER BY COALESCE(o.paid_at,o.created_at) DESC,o.id DESC LIMIT 11", test.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(detail, "TEMP B-TREE") {
					t.Errorf("public pagination sorts all matching orders: %s", detail)
				}
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
