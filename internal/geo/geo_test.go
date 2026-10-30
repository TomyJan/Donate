package geo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
)

type freshLocator interface {
	Refresh(context.Context, string) (Location, error)
}

func TestRefreshCacheRejectsOrdinaryLookupStartedDuringRefresh(t *testing.T) {
	for _, evict := range []bool{false, true} {
		t.Run(map[bool]string{false: "cached", true: "evicted"}[evict], func(t *testing.T) {
			s, err := New(config.Config{GeoMode: "online"}, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			s.sources = s.sources[:2]
			for _, src := range s.sources {
				src.interval = 0
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			freshStarted, releaseFresh := make(chan struct{}), make(chan struct{})
			ordinaryStarted, releaseOrdinary := make(chan struct{}), make(chan struct{})
			var primaryCalls atomic.Int32
			wait := func(ch <-chan struct{}) error {
				select {
				case <-ch:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			s.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
				body := `{"ret":200,"data":{"ip":"1.1.1.1","country":"中国","prov":"新省"}}`
				if strings.Contains(r.URL.Host, "ip9") {
					switch primaryCalls.Add(1) {
					case 1:
						return nil, errors.New("synthetic primary unavailable")
					case 2:
						close(ordinaryStarted)
						if err := wait(releaseOrdinary); err != nil {
							return nil, err
						}
						body = `{"ret":200,"data":{"ip":"1.1.1.1","country":"中国","prov":"旧省"}}`
					}
				} else {
					close(freshStarted)
					if err := wait(releaseFresh); err != nil {
						return nil, err
					}
					body = `{"ip":"1.1.1.1","geo":{"country":"中国","province":"新省"}}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			forcedDone := make(chan error, 1)
			go func() { _, err := s.Refresh(ctx, "1.1.1.1"); forcedDone <- err }()
			if err := wait(freshStarted); err != nil {
				t.Fatal(err)
			}
			ordinaryDone := make(chan error, 1)
			go func() { _, err := s.Lookup(ctx, "1.1.1.1"); ordinaryDone <- err }()
			if err := wait(ordinaryStarted); err != nil {
				t.Fatal(err)
			}
			close(releaseFresh)
			if err := <-forcedDone; err != nil {
				t.Fatal(err)
			}
			if evict {
				s.mu.Lock()
				delete(s.cache, "1.1.1.1")
				s.mu.Unlock()
			}
			close(releaseOrdinary)
			if err := <-ordinaryDone; err != nil {
				t.Fatal(err)
			}
			p, err := s.Lookup(ctx, "1.1.1.1")
			if err != nil || p.Province != "新省" {
				t.Fatalf("lookup started during refresh replaced fresh cache: province=%s err=%v", p.Province, err)
			}
		})
	}
}

func TestRefreshCacheRejectsOlderAutomaticLookup(t *testing.T) {
	for _, evict := range []bool{false, true} {
		t.Run(map[bool]string{false: "cached", true: "evicted"}[evict], func(t *testing.T) { testRefreshCacheRejectsOlderAutomaticLookup(t, evict) })
	}
}

func testRefreshCacheRejectsOlderAutomaticLookup(t *testing.T, evict bool) {
	s, err := New(config.Config{GeoMode: "online"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.sources = s.sources[:2]
	for _, src := range s.sources {
		src.interval = 0
	}
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	var primaryCalls atomic.Int32
	s.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		body := `{"ret":200,"data":{"ip":"1.1.1.1","country":"中国","prov":"新省"}}`
		if strings.Contains(r.URL.Host, "ip9") {
			if primaryCalls.Add(1) == 1 {
				return nil, errors.New("synthetic primary unavailable")
			}
		} else {
			close(oldStarted)
			<-releaseOld
			body = `{"ip":"1.1.1.1","geo":{"country":"中国","province":"旧省"}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	done := make(chan error, 1)
	go func() { _, err := s.Lookup(context.Background(), "1.1.1.1"); done <- err }()
	<-oldStarted
	fresh, err := s.Refresh(context.Background(), "1.1.1.1")
	if evict {
		s.mu.Lock()
		delete(s.cache, "1.1.1.1")
		s.mu.Unlock()
	}
	close(releaseOld)
	if err != nil || fresh.Province != "新省" {
		t.Fatal(fresh, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	cached, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || cached.Province != "新省" {
		t.Fatal("older lookup replaced refreshed cache", cached, err)
	}
}

func TestRefreshBypassesCacheAndHonorsDisabled(t *testing.T) {
	s, err := New(config.Config{GeoMode: "online"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fresh, ok := any(s).(freshLocator)
	if !ok {
		t.Fatal("geo service has no forced refresh capability")
	}
	calls := 0
	s.sources = s.sources[:1]
	s.sources[0].interval = 0
	s.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		province := "旧省"
		if calls > 1 {
			province = "新省"
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ret":200,"data":{"ip":"1.1.1.1","country":"中国","prov":"` + province + `"}}`))}, nil
	})
	old, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || old.Province != "旧省" {
		t.Fatal(old, err)
	}
	cached, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || cached.Province != "旧省" || calls != 1 {
		t.Fatal(cached, err, calls)
	}
	updated, err := fresh.Refresh(context.Background(), "1.1.1.1")
	if err != nil || updated.Province != "新省" || calls != 2 {
		t.Fatal(updated, err, calls)
	}
	s.config.GeoMode = "disabled"
	if _, err = fresh.Refresh(context.Background(), "1.1.1.1"); err == nil {
		t.Fatal("disabled refresh falsely succeeded")
	}
	s.config.GeoMode = "online"
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic offline") })
	if _, err = fresh.Refresh(context.Background(), "1.1.1.1"); err == nil {
		t.Fatal("failed refresh reused the old cache")
	}
}

func TestRefreshKeepsNormalCacheForOtherIPs(t *testing.T) {
	s, err := New(config.Config{GeoMode: "online"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.sources = s.sources[:1]
	s.sources[0].interval = 0
	calls := 0
	s.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"ret":200,"data":{"ip":"` + r.URL.Query().Get("ip") + `","country":"中国","prov":"合成省"}}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if _, err = s.Refresh(context.Background(), "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = s.Lookup(context.Background(), "1.0.0.1"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("ordinary lookup stopped caching after a refresh: calls=%d want 2", calls)
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPrivateAndReservedNeverLeaveProcess(t *testing.T) {
	s, e := New(config.Config{GeoMode: "online"}, slog.Default())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected external request %s", r.URL.Host)
		return nil, nil
	})
	for _, ip := range []string{"127.0.0.1", "10.1.1.1", "192.0.2.1", "::1", "fe80::1", "2001:db8::1", "100.64.1.1"} {
		if _, e = s.Lookup(context.Background(), ip); e != nil {
			t.Fatal(e)
		}
	}
}
func TestFailoverValidatesEchoAndCaches(t *testing.T) {
	s, e := New(config.Config{GeoMode: "online"}, slog.Default())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	calls := 0
	s.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"ret":200,"data":{"ip":"8.8.8.8","country":"中国","prov":"错误"}}`
		if strings.Contains(r.URL.Host, "ipyou") {
			body = `{"ip":"1.1.1.1","geo":{"country":"中国","province":"浙江","city":"杭州","isp":"电信"}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	p, e := s.Lookup(context.Background(), "1.1.1.1")
	if e != nil || p.Province != "浙江" || p.City != "杭州" || p.Source != "ipyou" {
		t.Fatalf("%+v %v", p, e)
	}
	if _, e = s.Lookup(context.Background(), "1.1.1.1"); e != nil || calls != 2 {
		t.Fatalf("cache missed %d %v", calls, e)
	}
}
