package config

import (
	"strings"
	"testing"
)

func TestLoadLocalGeoSources(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_URL", "http://localhost:8080")
	t.Setenv("GEO_MODE", "local")
	for _, tc := range []struct {
		name, v4, v6, mmdb string
		valid              bool
	}{
		{"v4 only", "./ip2region_v4.xdb", "", "", true},
		{"v6 only", "", "./ip2region_v6.xdb", "", true},
		{"mmdb compatible", "", "", "./City.mmdb", true},
		{"all sources", "./v4.xdb", "./v6.xdb", "./City.mmdb", true},
		{"empty", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GEO_XDB_IPV4", tc.v4)
			t.Setenv("GEO_XDB_IPV6", tc.v6)
			t.Setenv("GEO_DATABASE", tc.mmdb)
			c, err := Load()
			if !tc.valid {
				if err == nil || !strings.Contains(err.Error(), "GEO_") {
					t.Fatalf("missing local sources: %v", err)
				}
				return
			}
			if err != nil || c.GeoXDBv4 != tc.v4 || c.GeoXDBv6 != tc.v6 || c.GeoPath != tc.mmdb {
				t.Fatalf("sources=(%s,%s,%s) err=%v", c.GeoXDBv4, c.GeoXDBv6, c.GeoPath, err)
			}
		})
	}
}
