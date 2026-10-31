package geo

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

func TestXDBVectorUpdatesPreserveWorkingLibrary(t *testing.T) {
	for _, family := range []struct{ ip, boundary, nextPrefix string }{
		{"1.1.1.1", "1.1.255.255", "1.2.0.1"},
		{"240e::1", "240e:ffff:ffff:ffff:ffff:ffff:ffff:ffff", "240f::1"},
	} {
		for _, mode := range []string{"missing", "narrowed", "missing-cross-prefix", "valid-sparse", "valid-cross-prefix"} {
			t.Run(family.ip+"/"+mode, func(t *testing.T) {
				ip := netip.MustParseAddr(family.ip)
				path := syntheticXDB(t, family.ip, "中国|广东|深圳|旧数据|CN")
				before := mustReadDatabase(t, path)
				c := config.Config{GeoMode: "local", GeoXDBv4: path}
				libIndex := 0
				if ip.Is6() {
					c.GeoXDBv4, c.GeoXDBv6 = "", path
					libIndex = 1
				}
				s, err := New(c, slog.Default())
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				if p, err := s.Lookup(context.Background(), family.ip); err != nil || p.Province != "广东" {
					t.Fatal("old library is not usable", p, err)
				}
				ranges := [][2]netip.Addr{{ip, ip}, {ip.Next(), ip.Next()}}
				if mode == "missing-cross-prefix" || mode == "valid-cross-prefix" {
					ranges = [][2]netip.Addr{{ip, ip}, {netip.MustParseAddr(family.boundary), netip.MustParseAddr(family.nextPrefix)}}
				}
				data := mustReadDatabase(t, xdbIntervalFixture(t, ranges))
				start := binary.LittleEndian.Uint32(data[8:])
				end := binary.LittleEndian.Uint32(data[12:])
				first := xdbVectorOffset(ip)
				// Both intervals begin in the first prefix; the second may also
				// be reachable through another prefix's independent vector.
				if mode == "valid-cross-prefix" {
					next := xdbVectorOffset(netip.MustParseAddr(family.nextPrefix))
					binary.LittleEndian.PutUint32(data[next:], end)
					binary.LittleEndian.PutUint32(data[next+4:], end)
				}
				if mode == "missing" {
					clear(data[first : first+8])
				}
				if mode == "narrowed" {
					binary.LittleEndian.PutUint32(data[first:], end)
				}
				if start == end {
					t.Fatal("fixture must contain two distinct segments")
				}
				u := NewUpdater(s, c, slog.Default())
				u.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
				})
				lib := databaseSources(c)[libIndex]
				lib.url = "https://example.com/database"
				err = u.update(context.Background(), lib)
				valid := mode == "valid-sparse" || mode == "valid-cross-prefix"
				if !valid {
					if err == nil {
						t.Fatal("unreachable segments replaced the working library")
					}
					if !bytes.Equal(before, mustReadDatabase(t, path)) {
						t.Fatal("rejected update changed the original file")
					}
					if p, err := s.Refresh(context.Background(), family.ip); err != nil || p.Province != "广东" {
						t.Fatal("rejected update discarded the old reader", p, err)
					}
					return
				}
				if err != nil {
					t.Fatal("valid sparse or crossing vectors rejected", err)
				}
				for _, interval := range ranges {
					for _, address := range interval {
						if p, err := s.Refresh(context.Background(), address.String()); err != nil || p.Province != "浙江" {
							t.Fatal("updated segment is not reachable", address, p, err)
						}
					}
				}
			})
		}
	}
}

func xdbVectorOffset(ip netip.Addr) int {
	address := ip.AsSlice()
	return 256 + (int(address[0])*256+int(address[1]))*8
}
