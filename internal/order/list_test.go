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
