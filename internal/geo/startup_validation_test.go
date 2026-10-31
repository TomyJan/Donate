package geo

import (
	"bytes"
	"context"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

func TestExistingLocalDatabasesRequireFullStartupValidation(t *testing.T) {
	for _, mode := range []string{"corrupt-mmdb", "unsupported-mmdb", "truncated-xdb4", "truncated-xdb6", "missing-vector-xdb4", "missing-vector-xdb6"} {
		t.Run(mode, func(t *testing.T) {
			c := config.Config{GeoMode: "local"}
			var data []byte
			var path, name, reason string
			if strings.HasSuffix(mode, "mmdb") {
				path, name = filepath.Join(t.TempDir(), "existing.mmdb"), "GEO_DATABASE"
				c.GeoPath = path
				if mode == "corrupt-mmdb" {
					data = mustReadDatabase(t, "testdata/synthetic-city.mmdb")
					for i := range 6 {
						data[i] = 0xff
					}
					reason = "完整性"
				} else {
					data, reason = mmdbProduct(t, "GeoIP2-ISP"), "City"
				}
			} else {
				ip := "1.1.1.1"
				name = "GEO_XDB_IPV4"
				if strings.HasSuffix(mode, "xdb6") {
					ip, name = "240e::1", "GEO_XDB_IPV6"
				}
				path = syntheticXDB(t, ip, "中国|广东|深圳|网络|CN")
				data = mustReadDatabase(t, path)
				if strings.HasPrefix(mode, "truncated") {
					data, reason = data[:len(data)-1], "截断"
				} else {
					at := xdbVectorOffset(netip.MustParseAddr(ip))
					clear(data[at : at+8])
					reason = "未覆盖"
				}
				if strings.HasSuffix(mode, "xdb6") {
					c.GeoXDBv6 = path
				} else {
					c.GeoXDBv4 = path
				}
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := New(c, slog.Default())
			if err == nil {
				s.Close()
				t.Fatal("startup accepted an unusable local database")
			}
			if !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), strconv.Quote(path)) || !strings.Contains(err.Error(), reason) {
				t.Fatalf("startup error lacks file/config/integrity context: %v", err)
			}
			if got := mustReadDatabase(t, path); !bytes.Equal(got, data) {
				t.Fatal("startup validation changed the existing database")
			}
		})
	}
}

func TestSupportedMMDBProductsStartAndLocate(t *testing.T) {
	for _, product := range []string{"GeoIP2-City", "GeoLite2-City", "GeoIP2-Enterprise", "DBIP-City-Lite", "DBIP-City", "DBIP-Location", "DBIP-Location-ISP", "DBIP-Full"} {
		t.Run(product, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "city.mmdb")
			if err := os.WriteFile(path, mmdbProduct(t, product), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := New(config.Config{GeoMode: "local", GeoPath: path}, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if p, err := s.Lookup(context.Background(), "1.1.1.1"); err != nil || p.Country != "中国" || p.Province != "合成省" {
				t.Fatalf("valid startup lookup: %+v %v", p, err)
			}
		})
	}
}
