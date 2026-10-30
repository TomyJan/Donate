package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestLegacyOrdersKeepDonorSnapshotsWithoutLoginIdentities(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	orders := order.New(db, log, "gateway", "merchant", "v2")
	service := New(db, orders, config.Config{}, log)
	id := storage.NewID()
	_, err = db.DB.ExecContext(ctx, "INSERT INTO orders(id,merchant_order_no,amount_fen,channel,status,created_at,expires_at,paid_at,protocol,request_ip,donor_nickname,donor_avatar_url,donor_qq_number) VALUES(?, 'legacy-source-number',888,'qqpay','paid',1640995200,1640997000,NULL,'legacy','192.0.2.1','历史昵称','https://q1.qlogo.cn/g?b=qq&nk=123456&s=640','123456')", id[:])
	if err != nil {
		t.Fatal(err)
	}
	public, err := orders.List(ctx, "", false, "")
	if err != nil || len(public.Items) != 1 {
		t.Fatalf("public list: %+v %v", public, err)
	}
	if public.Items[0].Nickname != "历史昵称" || public.Items[0].AvatarURL != "https://q1.qlogo.cn/g?b=qq&nk=123456&s=640" || public.Items[0].IsMine {
		t.Fatalf("lost legacy identity: %+v", public.Items[0])
	}
	if public.Items[0].Date.Unix() != 1640995200 {
		t.Fatal("missing payment time must fall back to the known creation time")
	}
	overview, err := service.OverviewRange(ctx, "all", "", "")
	if err != nil || overview.PaidFen != 888 || len(overview.Daily) != 1 || overview.Daily[0].Label != "2022-01-01" {
		t.Fatalf("legacy statistics: %+v %v", overview, err)
	}
	page, err := service.OrdersList(ctx, Filter{Search: "历史昵称", Channel: "qqpay", Sort: "nickname", Direction: "asc", Size: 10})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Nickname != "历史昵称" || page.Items[0].Channel != "qqpay" || page.Items[0].TradeNo != "" {
		t.Fatalf("admin legacy list: %+v %v", page, err)
	}
	for _, full := range []bool{false, true} {
		detail, err := service.Detail(ctx, id.String(), full)
		if err != nil || detail.Nickname != "历史昵称" {
			t.Fatalf("detail: %+v %v", detail, err)
		}
		raw, _ := json.Marshal(detail)
		var wire map[string]any
		_ = json.Unmarshal(raw, &wire)
		if full && wire["qq_number"] != "123456" {
			t.Fatalf("admin QQ not preserved: %s", raw)
		}
		if !full && wire["qq_number"] != nil {
			t.Fatalf("private QQ exposed in public detail: %s", raw)
		}
	}
	var count int
	if _, err = db.DB.ExecContext(ctx, "UPDATE orders SET donor_nickname=? WHERE id=?", " \t　\n", id[:]); err != nil {
		t.Fatal(err)
	}
	public, err = orders.List(ctx, "", false, "")
	if err != nil || public.Items[0].Nickname != "捐赠者" {
		t.Fatalf("blank legacy nickname: %+v %v", public, err)
	}
	detail, err := service.Detail(ctx, id.String(), true)
	if err != nil || detail.Nickname != "捐赠者" {
		t.Fatalf("blank legacy detail nickname: %+v %v", detail, err)
	}
	if err = db.DB.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatal("legacy imports must not fabricate users", count, err)
	}
}
