package admin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/storage"
)

func TestGeoCheckOnlyExposedInAdminDetail(t *testing.T) {
	s, user := readFixture(t)
	o := addReadOrder(t, s, user, "geo-check", "paid", "2026-10-30T00:00:00Z", "2026-10-30T00:00:00Z", "alipay", "", 1, false)
	oid, _ := storage.ParseID(o.ID)
	check := geo.Location{Country: "中国", Province: "广东", Source: "xdb", Verification: "corroborated", QueriedAt: s.Now(), Evidence: []geo.Evidence{{Source: "ip9", Province: "广东", City: "不公开的候选城市", Outcome: "complete", QueriedAt: s.Now()}}}
	raw, _ := json.Marshal(check)
	if _, err := s.Store.DB.Exec("INSERT INTO order_location_checks(order_id,result_json) VALUES(?,?)", oid, string(raw)); err != nil {
		t.Fatal(err)
	}
	for _, full := range []bool{false, true} {
		d, err := s.Detail(context.Background(), o.ID, full)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "不公开的候选城市") != full {
			t.Fatalf("full=%t: %s", full, raw)
		}
	}
}
