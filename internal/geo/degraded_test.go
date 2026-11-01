package geo

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

func TestUnavailableLocalLibrariesStartAndUseOnlineUntilInstalled(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "corrupt"}[corrupt], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "city.mmdb")
			if corrupt {
				if err := os.WriteFile(path, []byte("invalid database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			c := config.Config{GeoMode: "local", GeoPath: path, GeoMMDBURL: "https://example.com/city.mmdb", GeoUpdateDays: 7}
			s, err := New(c, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatalf("unavailable local library blocked startup: %v", err)
			}
			defer s.Close()
			s.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ret":200,"data":{"ip":"223.5.5.5","country":"中国","prov":"江苏"}}`))}, nil
			})
			p, err := s.Lookup(context.Background(), "223.5.5.5")
			if err != nil || p.Province != "江苏" || p.Source != "ip9" {
				t.Fatalf("online degradation failed: %+v %v", p, err)
			}
			stage := filepath.Join(t.TempDir(), "download.mmdb")
			if err := os.WriteFile(stage, mustReadDatabase(t, "testdata/synthetic-city.mmdb"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.installDatabase(context.Background(), databaseSources(c)[2], stage); err != nil {
				t.Fatal(err)
			}
			s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
				t.Error("healthy local library unexpectedly used online lookup")
				return nil, ErrUnavailable
			})
			p, err = s.Lookup(context.Background(), "223.5.5.5")
			if err != nil || p.Source != "mmdb" {
				t.Fatalf("installed database did not replace degraded cache: %+v %v", p, err)
			}
		})
	}
}

func TestUnavailableLibraryDoesNotDiscardOtherHealthyLocalReader(t *testing.T) {
	s, err := New(config.Config{GeoMode: "local", GeoPath: "testdata/synthetic-city.mmdb", GeoXDBv4: filepath.Join(t.TempDir(), "missing.xdb")}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		t.Error("healthy MMDB should remain available")
		return nil, ErrUnavailable
	})
	p, err := s.Lookup(context.Background(), "223.5.5.5")
	if err != nil || p.Source != "mmdb" {
		t.Fatal(p, err)
	}
}

func TestIPv4MMDBDoesNotBlockDegradedIPv6Lookup(t *testing.T) {
	s, err := New(config.Config{GeoMode: "local", GeoPath: "testdata/synthetic-city.mmdb", GeoXDBv6: filepath.Join(t.TempDir(), "missing-v6.xdb")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{\"ret\":200,\"data\":{\"ip\":\"240e::1\",\"country\":\"中国\",\"prov\":\"江苏\"}}"))}, nil
	})
	p, err := s.Lookup(context.Background(), "240e::1")
	if err != nil || p.Province != "江苏" || p.Source != "ip9" {
		t.Fatalf("IPv4 MMDB suppressed IPv6 fallback: %+v %v", p, err)
	}
}
