package geo

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

func TestXDBRecordValidationRejectsMalformedContent(t *testing.T) {
	for _, family := range []struct{ ip, kind string }{{"1.1.1.1", "xdb4"}, {"240e::1", "xdb6"}} {
		for _, region := range []string{"中国:广东|深圳|网络|CN", "中国|广东|深圳|网络|CN|extra", "中国|广东|深圳|\xff|CN"} {
			t.Run(family.kind+"/"+region, func(t *testing.T) {
				path := syntheticXDB(t, family.ip, region)
				if err := validateDatabase(path, family.kind); err == nil {
					t.Fatal("malformed region passed validation")
				}
			})
		}
	}
}

func TestXDBRecordValidationPreservesSupportedAndEmptyRecords(t *testing.T) {
	for _, ip := range []string{"1.1.1.1", "240e::1"} {
		for _, region := range []string{"中国|广东|深圳|网络|CN", "0|0|0|0|0", "||||", ""} {
			kind := "xdb4"
			if ip == "240e::1" {
				kind = "xdb6"
			}
			if err := validateDatabase(syntheticXDB(t, ip, region), kind); err != nil {
				t.Fatalf("valid %s record %q rejected: %v", kind, region, err)
			}
		}
	}
	path := syntheticXDB(t, "1.1.1.1", "中国|0|广东省|深圳市|电信")
	data := mustReadDatabase(t, path)
	binary.LittleEndian.PutUint16(data, xdb.Structure20)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateDatabase(path, "xdb4"); err != nil {
		t.Fatal("legacy record rejected", err)
	}
	s, err := New(config.Config{GeoMode: "local", GeoXDBv4: path}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if p, err := s.Lookup(context.Background(), "1.1.1.1"); err != nil || p.Province != "广东" || p.ISP != "电信" {
		t.Fatalf("legacy lookup changed: %+v %v", p, err)
	}
}

func TestXDBRecordValidationChecksEveryPayloadAndLength(t *testing.T) {
	for _, family := range []struct{ ip, kind string }{{"1.1.1.1", "xdb4"}, {"240e::1", "xdb6"}} {
		for _, mode := range []string{"shared", "later-malformed", "same-offset-shorter", "empty-then-malformed"} {
			t.Run(family.kind+"/"+mode, func(t *testing.T) {
				ip := netip.MustParseAddr(family.ip)
				path := xdbIntervalFixture(t, [][2]netip.Addr{{ip, ip}, {ip.Next(), ip.Next()}})
				data := mustReadDatabase(t, path)
				index := int(binary.LittleEndian.Uint32(data[8:]))
				width := len(ip.AsSlice())
				second := index + width*2 + 6
				if mode == "same-offset-shorter" {
					length := binary.LittleEndian.Uint16(data[second+width*2:])
					binary.LittleEndian.PutUint16(data[second+width*2:], length-3)
				} else if mode != "shared" {
					const malformed = "中国:浙江|杭州|新数据|CN"
					binary.LittleEndian.PutUint16(data[second+width*2:], uint16(len(malformed)))
					binary.LittleEndian.PutUint32(data[second+width*2+2:], uint32(len(data)))
					data = append(data, malformed...)
					if mode == "empty-then-malformed" {
						binary.LittleEndian.PutUint16(data[index+width*2:], 0)
					}
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				err := validateDatabase(path, family.kind)
				if mode == "shared" && err != nil {
					t.Fatal("shared valid record rejected", err)
				}
				if mode != "shared" && err == nil {
					t.Fatal("later malformed record was skipped")
				}
			})
		}
	}
}

func TestCorruptXDBRecordsDegradeAndBecomeImmediatelyDue(t *testing.T) {
	for _, family := range []struct{ ip, kind string }{{"1.1.1.1", "xdb4"}, {"240e::1", "xdb6"}} {
		t.Run(family.kind, func(t *testing.T) {
			path := syntheticXDB(t, family.ip, "中国:广东|深圳|网络|CN")
			c := config.Config{GeoMode: "local", GeoUpdateDays: 7, GeoXDBv4: path, GeoXDBv4URL: "https://example.invalid/ipv4.xdb"}
			if family.kind == "xdb6" {
				c.GeoXDBv4, c.GeoXDBv4URL, c.GeoXDBv6, c.GeoXDBv6URL = "", "", path, "https://example.invalid/ipv6.xdb"
			}
			s, err := New(c, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatalf("bad library blocked startup: %v", err)
			}
			defer s.Close()
			if s.libraryReady(family.kind) {
				t.Fatal("malformed library activated")
			}
			due := NewUpdater(s, c, nil).Due(time.Now())
			if len(due) != 1 || due[0] != family.kind {
				t.Fatalf("corrupt library not immediately scheduled: %v", due)
			}
			body, err := json.Marshal(map[string]any{"ret": 200, "data": map[string]string{"ip": family.ip, "country": "中国", "prov": "江苏"}})
			if err != nil {
				t.Fatal(err)
			}
			s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
			})
			if p, err := s.Lookup(context.Background(), family.ip); err != nil || p.Province != "江苏" {
				t.Fatalf("startup did not fall back online: %+v %v", p, err)
			}
		})
	}
}

func TestCorruptXDBRecordUpdateKeepsHealthyLibrary(t *testing.T) {
	for _, family := range []struct{ ip, kind string }{{"1.1.1.1", "xdb4"}, {"240e::1", "xdb6"}} {
		t.Run(family.kind, func(t *testing.T) {
			path := syntheticXDB(t, family.ip, "中国|广东|深圳|旧数据|CN")
			before := mustReadDatabase(t, path)
			c, libIndex := config.Config{GeoMode: "local", GeoXDBv4: path}, 0
			if family.kind == "xdb6" {
				c.GeoXDBv4, c.GeoXDBv6, libIndex = "", path, 1
			}
			s, err := New(c, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			stage := syntheticXDB(t, family.ip, "中国:浙江|杭州|新数据|CN")
			if err := s.installDatabase(context.Background(), databaseSources(c)[libIndex], stage); err == nil {
				t.Fatal("corrupt record update accepted")
			}
			if !bytes.Equal(before, mustReadDatabase(t, path)) {
				t.Fatal("rejected update changed the original file")
			}
			if p, err := s.Refresh(context.Background(), family.ip); err != nil || p.Province != "广东" || p.ISP != "旧数据" {
				t.Fatalf("healthy reader was lost: %+v %v", p, err)
			}
		})
	}
}
