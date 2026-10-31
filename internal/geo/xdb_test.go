package geo

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

// One fabricated record in a real XDB v3 file, built without external data.
func syntheticXDB(t *testing.T, ip, region string) string {
	t.Helper()
	a := netip.MustParseAddr(ip)
	raw := a.AsSlice()
	size, version := 14, 4
	if a.Is6() {
		size, version = 38, 6
	}
	index := 256 + 256*256*8
	data := index + size
	b := make([]byte, data+len(region))
	binary.LittleEndian.PutUint16(b, 3)
	binary.LittleEndian.PutUint16(b[2:], 1)
	binary.LittleEndian.PutUint32(b[4:], 1700000000)
	binary.LittleEndian.PutUint32(b[8:], uint32(index))
	binary.LittleEndian.PutUint32(b[12:], uint32(index))
	binary.LittleEndian.PutUint16(b[16:], uint16(version))
	binary.LittleEndian.PutUint16(b[18:], 4)
	vector := 256 + (int(raw[0])*256+int(raw[1]))*8
	binary.LittleEndian.PutUint32(b[vector:], uint32(index))
	binary.LittleEndian.PutUint32(b[vector+4:], uint32(index))
	address := append([]byte(nil), raw...)
	if a.Is4() {
		for i, j := 0, 3; i < j; i, j = i+1, j-1 {
			address[i], address[j] = address[j], address[i]
		}
	}
	copy(b[index:], address)
	copy(b[index+len(raw):], address)
	binary.LittleEndian.PutUint16(b[index+len(raw)*2:], uint16(len(region)))
	binary.LittleEndian.PutUint32(b[index+len(raw)*2+2:], uint32(data))
	copy(b[data:], region)
	path := filepath.Join(t.TempDir(), "synthetic.xdb")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestXDBDualStackPriorityAndNoNetwork(t *testing.T) {
	v4 := syntheticXDB(t, "1.1.1.1", "中国|广东省|深圳市|测试网络|CN")
	v6 := syntheticXDB(t, "240e::1", "中国|浙江省|杭州市|合成网络|CN")
	s, err := New(config.Config{GeoMode: "local", GeoXDBv4: v4, GeoXDBv6: v6, GeoPath: "testdata/synthetic-province.mmdb"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		t.Error("offline lookup leaked IP")
		return nil, ErrUnavailable
	})
	for _, tc := range []struct{ ip, province, city, isp string }{
		{"1.1.1.1", "广东", "深圳市", "测试网络"},
		{"::ffff:1.1.1.1", "广东", "深圳市", "测试网络"},
		{"240e::1", "浙江", "杭州市", "合成网络"},
	} {
		p, e := s.Lookup(context.Background(), tc.ip)
		if e != nil || p.Source != "xdb" || p.Province != tc.province || p.City != tc.city || p.ISP != tc.isp || p.Version == "" {
			t.Fatalf("%s: %+v %v", tc.ip, p, e)
		}
		if p.Latitude != nil || p.AccuracyRadius != nil {
			t.Fatal("XDB inherited MMDB coordinates")
		}
	}
}

func TestXDBMissingProvinceUsesMMDB(t *testing.T) {
	path := syntheticXDB(t, "1.1.1.1", "中国|0|0|0|CN")
	s, err := New(config.Config{GeoMode: "local", GeoXDBv4: path, GeoPath: "testdata/synthetic-province.mmdb"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || p.Source != "mmdb" || p.Province != "合成省" {
		t.Fatalf("fallback: %+v %v", p, err)
	}
}

func TestXDBRejectsInvalidFiles(t *testing.T) {
	v4 := syntheticXDB(t, "1.1.1.1", "中国|浙江|杭州|测试|CN")
	short := filepath.Join(t.TempDir(), "short.xdb")
	_ = os.WriteFile(short, []byte("not an xdb"), 0600)
	for _, tc := range []config.Config{
		{GeoMode: "local", GeoXDBv4: short},
		{GeoMode: "local", GeoXDBv4: filepath.Join(t.TempDir(), "missing.xdb")},
		{GeoMode: "local", GeoXDBv6: v4},
		{GeoMode: "local", GeoXDBv4: v4, GeoPath: short},
	} {
		s, e := New(tc, slog.Default())
		if e == nil {
			s.Close()
			t.Fatal("invalid configured source silently ignored")
		}
	}
}

func TestXDBConcurrentLookupAndIdempotentClose(t *testing.T) {
	s, err := New(config.Config{GeoMode: "local", GeoXDBv4: syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|测试|CN")}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			p, e := s.Refresh(context.Background(), "1.1.1.1")
			if e != nil || p.Source != "xdb" {
				t.Errorf("%+v %v", p, e)
			}
		})
	}
	wg.Wait()
	s.Close()
	s.Close()
}
