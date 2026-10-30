package order

import (
	"context"
	"encoding/json"
	"github.com/TomyJan/Donate/internal/storage"
	"strings"
	"testing"
	"time"
)

func TestGuestLimitAndPublicPrivacy(t *testing.T) {
	s, u := setup(t)
	uid, _ := storage.ParseID(u)
	for i := range 15 {
		id := storage.NewID()
		_, e := s.Store.DB.Exec("INSERT INTO orders(id,user_id,merchant_order_no,amount_fen,channel,status,created_at,expires_at,paid_at,trade_no,request_ip) VALUES(?,?,?,1,'alipay','paid',?,?,?,?, '8.8.8.8')", id[:], uid, id.String(), time.Now().Unix()+int64(i), time.Now().Unix()+1800, time.Now().Unix()+int64(i), id.String())
		if e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.List(context.Background(), "", false, "malicious")
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Items) != 10 || page.Next != "" {
		t.Fatal("guest limit bypass")
	}
	b, _ := json.Marshal(page)
	for _, secret := range []string{"request_ip", "8.8.8.8", "user_id", "subject", "trade_no"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("public privacy", secret)
		}
	}
	if _, e = s.List(context.Background(), "", true, ""); e != ErrForbidden {
		t.Fatal(e)
	}
	first, e := s.List(context.Background(), u, true, "")
	if e != nil || first.Next == "" {
		t.Fatal(e)
	}
	second, e := s.List(context.Background(), u, true, first.Next)
	if e != nil || len(second.Items) != 5 {
		t.Fatalf("pagination %d %v", len(second.Items), e)
	}
}

func TestRecordOwnershipDoesNotBroadenPublicVisibility(t *testing.T) {
	s, owner := setup(t)
	ctx := context.Background()
	other := storage.NewID()
	if _, err := s.Store.DB.ExecContext(ctx, "INSERT INTO users(id,created_at) VALUES(?,?)", other[:], time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	add := func(user, key, status string, hiddenOrder, hiddenMessage bool) Order {
		t.Helper()
		o := create(t, s, user, key)
		id, _ := storage.ParseID(o.ID)
		var paid, trade any
		if status == "paid" {
			paid = time.Now().Unix()
			trade = key
		}
		if _, err := s.Store.DB.ExecContext(ctx, "UPDATE orders SET status=?,paid_at=?,trade_no=?,hidden_order=?,hidden_message=? WHERE id=?", status, paid, trade, hiddenOrder, hiddenMessage, id); err != nil {
			t.Fatal(err)
		}
		return o
	}
	owned := add(owner, "own-public", "paid", false, true)
	foreign := add(other.String(), "other-public", "paid", false, true)
	add(owner, "own-pending", "pending", false, false)
	add(other.String(), "other-pending", "pending", false, false)
	add(owner, "own-hidden", "paid", true, false)
	oid, _ := storage.ParseID(owned.ID)
	lid := storage.NewID()
	if _, err := s.Store.DB.ExecContext(ctx, "INSERT INTO order_locations(id,order_id,country,province,city,street,source,queried_at) VALUES(?,?,'中国','上海','合成测试城市','不公开街道','fixture-synthetic',?)", lid[:], oid, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	for _, viewer := range []string{"", owner, other.String()} {
		got, err := s.List(ctx, viewer, false, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Items) != 2 {
			t.Fatalf("public viewer=%q got %d records", viewer, len(got.Items))
		}
		for _, item := range got.Items {
			raw, _ := json.Marshal(item)
			var wire map[string]any
			_ = json.Unmarshal(raw, &wire)
			isMine, ok := wire["is_mine"].(bool)
			if !ok {
				t.Fatalf("missing is_mine: %s", raw)
			}
			wantMine := (item.ID == owned.ID && viewer == owner) || (item.ID == foreign.ID && viewer == other.String())
			if isMine != wantMine {
				t.Errorf("viewer=%q item=%s is_mine=%v", viewer, item.ID, isMine)
			}
			if (item.Message != "") != wantMine {
				t.Errorf("hidden message ownership=%v: %s", wantMine, raw)
			}
			if item.Status != "paid" {
				t.Errorf("non-paid public record=%s", raw)
			}
			if item.ID == owned.ID && item.Province != "上海" {
				t.Errorf("province=%q", item.Province)
			}
			for _, secret := range []string{"user_id", "request_ip", "location", "合成测试城市", "不公开街道"} {
				if strings.Contains(string(raw), secret) {
					t.Errorf("leaked %q in %s", secret, raw)
				}
			}
		}
	}
	mine, err := s.List(ctx, owner, true, "")
	if err != nil || len(mine.Items) != 3 {
		t.Fatalf("own private records=%+v error=%v", mine, err)
	}
	for _, item := range mine.Items {
		raw, _ := json.Marshal(item)
		var wire map[string]any
		_ = json.Unmarshal(raw, &wire)
		if wire["is_mine"] != true {
			t.Errorf("private record ownership=%s", raw)
		}
	}
}
