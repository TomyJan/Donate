package geo

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

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
