package geo

import (
	"bytes"
	"context"
	"encoding/binary"
	"log/slog"
	"net/netip"
	"os"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

func TestXDBIntervalUpdatesPreserveWorkingLibrary(t *testing.T) {
	for _, address := range []string{"1.1.1.255", "240e::ff"} {
		ip := netip.MustParseAddr(address)
		next := ip.Next()
		for _, tc := range []struct {
			name   string
			ranges [][2]netip.Addr
			valid  bool
		}{
			{"reversed", [][2]netip.Addr{{next, ip}}, false},
			{"overlapping", [][2]netip.Addr{{ip, next}, {next, next}}, false},
			{"unsorted", [][2]netip.Addr{{next, next}, {ip, ip}}, false},
			{"adjacent", [][2]netip.Addr{{ip, ip}, {next, next}}, true},
		} {
			t.Run(address+"/"+tc.name, func(t *testing.T) {
				path := syntheticXDB(t, address, "中国|广东|深圳|旧数据|CN")
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
				stage := xdbIntervalFixture(t, tc.ranges)
				err = s.installDatabase(context.Background(), databaseSources(c)[libIndex], stage)
				wantProvince := "广东"
				if tc.valid {
					if err != nil {
						t.Fatalf("valid adjacent intervals rejected: %v", err)
					}
					wantProvince = "浙江"
				} else {
					if err == nil {
						t.Fatal("invalid IP intervals replaced the working library")
					}
					if !bytes.Equal(before, mustReadDatabase(t, path)) {
						t.Fatal("rejected update changed the original file")
					}
				}
				p, err := s.Refresh(context.Background(), address)
				if err != nil || p.Province != wantProvince {
					t.Fatalf("live reader: %+v %v", p, err)
				}
				if tc.valid {
					p, err = s.Refresh(context.Background(), next.String())
					if err != nil || p.Province != wantProvince {
						t.Fatalf("second adjacent interval: %+v %v", p, err)
					}
				}
			})
		}
	}
}

func xdbIntervalFixture(t *testing.T, ranges [][2]netip.Addr) string {
	t.Helper()
	const region = "中国|浙江|杭州|新数据|CN"
	path := syntheticXDB(t, ranges[0][0].String(), region)
	raw := mustReadDatabase(t, path)
	index := int(binary.LittleEndian.Uint32(raw[8:]))
	width := len(ranges[0][0].AsSlice())
	size := width*2 + 6
	data := index + len(ranges)*size
	out := make([]byte, data+len(region))
	copy(out, raw[:index])
	binary.LittleEndian.PutUint32(out[12:], uint32(index+(len(ranges)-1)*size))
	for i, interval := range ranges {
		segment := out[index+i*size:]
		for j, ip := range interval {
			address := ip.AsSlice()
			if ip.Is4() {
				address[0], address[1], address[2], address[3] = address[3], address[2], address[1], address[0]
			}
			copy(segment[j*width:], address)
		}
		binary.LittleEndian.PutUint16(segment[width*2:], uint16(len(region)))
		binary.LittleEndian.PutUint32(segment[width*2+2:], uint32(data))
	}
	address := ranges[0][0].AsSlice()
	vector := 256 + (int(address[0])*256+int(address[1]))*8
	binary.LittleEndian.PutUint32(out[vector+4:], uint32(index+(len(ranges)-1)*size))
	copy(out[data:], region)
	if err := os.WriteFile(path, out, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
