package geo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

func TestLocalVerificationSelectsCoherentSource(t *testing.T) {
	for _, tc := range []struct {
		name, first, second, source, province, verification string
		calls                                               int
		conflict                                            bool
	}{
		{"agreement", "广东省", "", "xdb", "广东", "corroborated", 1, false},
		{"local corroborated after disagreement", "浙江省", "广东", "xdb", "广东", "corroborated", 2, true},
		{"two online corroborate another province", "浙江省", "浙江", "ip9", "浙江", "corroborated", 2, true},
		{"unresolved", "浙江", "江苏", "unknown", "", "conflict", 2, true},
		{"outage keeps local", "error", "error", "xdb", "广东", "single", 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := New(config.Config{GeoMode: "local", GeoXDBv4: syntheticXDB(t, "1.1.1.1", "中国|广东省|深圳市|本地网络|CN"), GeoOnlineFallback: true}, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for _, src := range s.sources {
				src.interval = 0
			}
			calls := 0
			s.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
				calls++
				province := tc.first
				if calls > 1 {
					province = tc.second
				}
				if province == "error" {
					return nil, errors.New("synthetic offline")
				}
				if calls > 2 {
					t.Error("verification exceeded two complete online candidates")
				}
				body := fmt.Sprintf("{\"ret\":200,\"data\":{\"ip\":\"1.1.1.1\",\"country\":\"China\",\"prov\":%q,\"city\":\"在线城市\",\"latitude\":1}}", province)
				if strings.Contains(r.URL.Host, "ipyou") {
					body = fmt.Sprintf("{\"ip\":\"1.1.1.1\",\"geo\":{\"country\":\"中国\",\"province\":%q,\"city\":\"第二城市\"}}", province)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			p, err := s.Lookup(context.Background(), "1.1.1.1")
			if err != nil || p.Source != tc.source || p.Province != tc.province || p.Verification != tc.verification || p.Conflict != tc.conflict || calls != tc.calls {
				t.Fatalf("%+v calls=%d err=%v", p, calls, err)
			}
			if p.Source == "xdb" && (p.City != "深圳市" || p.Latitude != nil) {
				t.Fatal("mixed local and remote geographic fields", p)
			}
			if p.Verification == "conflict" && (p.City != "" || p.Latitude != nil) {
				t.Fatal("unresolved conflict exposed precise fields", p)
			}
			if len(p.Evidence) < 2 {
				t.Fatal("missing candidate diagnostics", p)
			}
			// Diagnostics returned to one caller must never mutate a cached decision.
			p.Evidence[0].Province = "poisoned"
			cached, e := s.Lookup(context.Background(), "1.1.1.1")
			if e != nil || cached.Evidence[0].Province == "poisoned" || calls != tc.calls {
				t.Fatal("cache lost isolation", cached, e, calls)
			}
			if tc.verification == "conflict" {
				calls = 0
				if _, e = s.Refresh(context.Background(), "1.1.1.1"); !errors.Is(e, ErrConflict) {
					t.Fatalf("conflicting refresh must preserve old snapshot: %v", e)
				}
			}
		})
	}
}

func TestNormalizeLocationAliasesAndUnknowns(t *testing.T) {
	for _, tc := range []struct{ country, province, want string }{
		{"China", "广东省", "广东"}, {"CN", "Shanghai", "上海"}, {"中国", "广西壮族自治区", "广西"},
		{"中国", "内蒙古自治区", "内蒙古"}, {"中国", "0", ""}, {"中国", "未知", ""}, {"中国", "合成省", "合成省"},
	} {
		p := normalize(Location{Country: tc.country, Province: tc.province})
		if p.Country != "中国" || p.Province != tc.want {
			t.Fatalf("%+v want %s", p, tc.want)
		}
	}
}

func TestXDBParsesModernAndLegacyLayouts(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		raw := "中国|广东省|深圳市|电信|CN"
		if legacy {
			raw = "中国|0|广东省|深圳市|电信"
		}
		p, e := parseXDB(raw, "test", legacy)
		if e != nil || p.Country != "中国" || p.Province != "广东" || p.City != "深圳市" || p.ISP != "电信" {
			t.Fatalf("legacy=%t %+v %v", legacy, p, e)
		}
	}
	p, e := parseXDB("中国|0|0|0|CN", "test", false)
	if e != nil || p.Province != "" || p.City != "" || p.ISP != "" {
		t.Fatal(p, e)
	}
	if _, e = parseXDB("中国|广东|深圳", "test", false); e == nil {
		t.Fatal("unsupported layout silently accepted")
	}
}
