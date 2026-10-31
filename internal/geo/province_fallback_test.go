package geo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
)

func TestLocalMissingProvinceFallsBackOnline(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "lookup", true: "refresh"}[refresh], func(t *testing.T) {
			s, err := New(config.Config{GeoMode: "local", GeoPath: "testdata/synthetic-city.mmdb", GeoOnlineFallback: true}, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			s.sources = s.sources[:1]
			calls := 0
			s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
				calls++
				body := `{"ret":200,"data":{"ip":"1.1.1.1","country":"中国","prov":"浙江","city":"杭州"}}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			lookup := s.Lookup
			if refresh {
				lookup = s.Refresh
			}
			p, err := lookup(context.Background(), "1.1.1.1")
			if err != nil || p.Province != "浙江" || p.City != "杭州" || p.Source != "ip9" || calls != 1 {
				t.Fatalf("missing province did not fall back: location=%+v calls=%d err=%v", p, calls, err)
			}
			if p.Latitude != nil || p.Version != "" {
				t.Fatal("online result inherited unrelated MMDB fields", p)
			}
		})
	}
}

func TestOnlineMissingProvinceContinuesAndCachesCompleteResult(t *testing.T) {
	s, err := New(config.Config{GeoMode: "online"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	calls := 0
	s.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"ret":200,"data":{"ip":"1.1.1.1","country":"中国"}}`
		if strings.Contains(r.URL.Host, "ipyou") {
			body = `{"ip":"1.1.1.1","geo":{"country":"中国","province":"浙江"}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	for range 2 {
		p, err := s.Lookup(context.Background(), "1.1.1.1")
		if err != nil || p.Province != "浙江" || p.Source != "ipyou" || calls != 2 {
			t.Fatalf("incomplete online result stopped fallback: location=%+v calls=%d err=%v", p, calls, err)
		}
	}
}

func TestLocalMissingProvinceKeepsPartialResultWhenOnlineFails(t *testing.T) {
	s, err := New(config.Config{GeoMode: "local", GeoPath: "testdata/synthetic-city.mmdb", GeoOnlineFallback: true}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	calls := 0
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("synthetic offline")
	})
	p, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || p.Country != "中国" || p.Source != "mmdb" || calls != len(s.sources) {
		t.Fatalf("partial local result lost or fallback skipped: location=%+v calls=%d err=%v", p, calls, err)
	}
	if ttl := time.Until(s.cache["1.1.1.1"].until); ttl > 5*time.Minute {
		t.Fatalf("incomplete result cached too long: %s", ttl)
	}
}

func TestIncompleteRefreshFailsWithoutEnablingNetwork(t *testing.T) {
	s, err := New(config.Config{GeoMode: "local", GeoPath: "testdata/synthetic-city.mmdb"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("offline configuration sent an external request")
		return nil, nil
	})
	p, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || p.Country != "中国" {
		t.Fatal("initial lookup lost partial offline data", p, err)
	}
	p, err = s.Refresh(context.Background(), "1.1.1.1")
	if err == nil || p.Country != "中国" || p.Province != "" {
		t.Fatal("country-only refresh falsely succeeded", p, err)
	}
}

func TestLocalProvinceAvoidsOnlineRequests(t *testing.T) {
	s, err := New(config.Config{GeoMode: "local", GeoPath: "testdata/synthetic-province.mmdb", GeoOnlineFallback: false}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("complete local result triggered an external request")
		return nil, nil
	})
	for _, lookup := range []func(context.Context, string) (Location, error){s.Lookup, s.Refresh} {
		p, err := lookup(context.Background(), "1.1.1.1")
		if err != nil || p.Province != "合成省" || p.Source != "mmdb" {
			t.Fatal("complete local result rejected", p, err)
		}
	}
}

func TestOnlineMissingProvinceTriesEverySourceAndKeepsFirstPartial(t *testing.T) {
	s, err := New(config.Config{GeoMode: "online"}, slog.Default())
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
		body := `{"ret":200,"data":{"ip":"1.1.1.1","country":"中国"}}`
		switch {
		case strings.Contains(r.URL.Host, "ipyou"):
			body = `{"ip":"1.1.1.1","geo":{"country":"China"}}`
		case strings.Contains(r.URL.Host, "i5p"):
			body = `{"code":0,"data":{"ip":"1.1.1.1","country":"China"}}`
		case strings.Contains(r.URL.Host, "pconline"):
			body = `{"ip":"1.1.1.1","country":"China"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	p, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || p.Country != "中国" || p.Source != "ip9" || calls != len(s.sources) {
		t.Fatalf("partial result lost or fallback skipped: location=%+v calls=%d err=%v", p, calls, err)
	}
	p, err = s.Refresh(context.Background(), "1.1.1.1")
	if err == nil || p.Country != "中国" || calls != 2*len(s.sources) {
		t.Fatalf("incomplete forced refresh falsely succeeded: location=%+v calls=%d err=%v", p, calls, err)
	}
	for _, src := range s.sources {
		if src.failures != 0 {
			t.Fatalf("sparse location incorrectly tripped source circuit breaker: %s failures=%d", src.name, src.failures)
		}
	}
}
