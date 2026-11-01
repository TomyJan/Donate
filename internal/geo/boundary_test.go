package geo

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
)

func TestXDBRejectsOutOfBoundsIndex(t *testing.T) {
	path := syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|网络|CN")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(raw[8:], uint32(len(raw)+100))
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateDatabase(path, "xdb4"); err == nil {
		t.Fatal("invalid index accepted")
	}
}

func TestXDBAcceptsOfficialTerminalSentinel(t *testing.T) {
	for _, ip := range []string{"255.255.255.255", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
		t.Run(ip, func(t *testing.T) {
			path := syntheticXDB(t, ip, "中国|广东|深圳|测试|CN")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			size := uint32(14)
			c := config.Config{GeoMode: "local", GeoXDBv4: path}
			if netip.MustParseAddr(ip).Is6() {
				size = 38
				c.GeoXDBv4, c.GeoXDBv6 = "", path
			}
			// Official files end their final vector with the exclusive segment
			// boundary, whereas the reader searches using an inclusive boundary.
			end := binary.LittleEndian.Uint32(raw[12:])
			binary.LittleEndian.PutUint32(raw[256+65535*8+4:], end+size)
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := New(c, slog.Default())
			if err != nil {
				t.Fatalf("official terminal vector rejected: %v", err)
			}
			defer s.Close()
			x := s.xdb4
			if c.GeoXDBv6 != "" {
				x = s.xdb6
			}
			p, err := x.lookup(netip.MustParseAddr(ip))
			if err != nil || p.Province != "广东" {
				t.Fatal(p, err)
			}
		})
	}
}

func TestXDBRejectsOtherExclusiveVectorBounds(t *testing.T) {
	for _, ip := range []string{"1.1.1.1", "240e::1"} {
		for _, terminal := range []bool{false, true} {
			name := ip + "/nonterminal"
			if terminal {
				name = ip + "/beyond-terminal-sentinel"
			}
			t.Run(name, func(t *testing.T) {
				path := syntheticXDB(t, ip, "中国|广东|深圳|测试|CN")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				a := netip.MustParseAddr(ip)
				size := uint32(14)
				c := config.Config{GeoMode: "local", GeoXDBv4: path}
				if a.Is6() {
					size = 38
					c.GeoXDBv4, c.GeoXDBv6 = "", path
				}
				addr := a.AsSlice()
				vector := 256 + (int(addr[0])*256+int(addr[1]))*8
				end := binary.LittleEndian.Uint32(raw[12:])
				bound := end + size
				if terminal {
					vector = 256 + 65535*8
					bound = end + 2*size
				}
				binary.LittleEndian.PutUint32(raw[vector:], end)
				binary.LittleEndian.PutUint32(raw[vector+4:], bound)
				if err = os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				kind := "xdb4"
				if a.Is6() {
					kind = "xdb6"
				}
				if err := validateDatabase(path, kind); err == nil {
					t.Fatal("out-of-bounds vector accepted")
				}
			})
		}
	}
}

func TestXDBLegacyFileAndMissingIPFamily(t *testing.T) {
	path := syntheticXDB(t, "1.1.1.1", "中国|0|广东省|深圳市|测试网络")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint16(raw, 2)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(config.Config{GeoMode: "local", GeoXDBv4: path}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || p.Province != "广东" || p.City != "深圳市" || p.ISP != "测试网络" {
		t.Fatal(p, err)
	}
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{\"ret\":200,\"data\":{\"ip\":\"240e::1\",\"country\":\"中国\",\"prov\":\"江苏\"}}"))}, nil
	})
	if p, err := s.Lookup(context.Background(), "240e::1"); err != nil || p.Source != "ip9" || p.Province != "江苏" {
		t.Fatalf("missing IP family did not degrade online: %+v %v", p, err)
	}
	s.Close()
	if _, err = s.Lookup(context.Background(), "1.1.1.1"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed reader served cached success", err)
	}
}

func TestUnavailableVerificationRetriesBeforeFullCacheTTL(t *testing.T) {
	s, err := New(config.Config{GeoMode: "local", GeoXDBv4: syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|测试|CN"), GeoOnlineFallback: true}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) { return nil, ErrUnavailable })
	if _, err = s.Lookup(context.Background(), "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	if ttl := time.Until(s.cache["1.1.1.1"].until); ttl > 5*time.Minute {
		t.Fatalf("unverified candidate cached too long: %s", ttl)
	}
}

func TestProviderQueueHonorsWaitingContext(t *testing.T) {
	s, err := New(config.Config{GeoMode: "online"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ret":200,"data":{"ip":"1.1.1.1","country":"中国","prov":"广东"}}`))}, nil
	})
	first := make(chan struct{})
	go func() { defer close(first); _, _ = s.lookupOnline(context.Background(), s.sources[0], "1.1.1.1") }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.lookupOnline(ctx, s.sources[0], "1.0.0.1"); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("provider queue ignored cancelled context")
	}
}

func TestParseRejectsInvalidCoordinates(t *testing.T) {
	p, err := parse("ip9", []byte(`{"ret":200,"data":{"ip":"1.1.1.1","country":"中国","prov":"广东","lat":"NaN","lng":"181"}}`), "1.1.1.1")
	if err != nil || p.Latitude != nil || p.Longitude != nil {
		t.Fatalf("invalid coordinates: %+v %v", p, err)
	}
}
