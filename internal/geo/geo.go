package geo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/oschwald/maxminddb-golang/v2"
	"golang.org/x/sync/singleflight"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var ErrUnavailable = errors.New("属地查询暂不可用")

type Location struct {
	Country        string    `json:"country"`
	Province       string    `json:"province"`
	City           string    `json:"city"`
	District       string    `json:"district"`
	Street         string    `json:"street"`
	ISP            string    `json:"isp"`
	ASN            string    `json:"asn"`
	Latitude       *float64  `json:"latitude,omitempty"`
	Longitude      *float64  `json:"longitude,omitempty"`
	AccuracyRadius *float64  `json:"accuracy_radius,omitempty"`
	Source         string    `json:"source"`
	Version        string    `json:"version"`
	QueriedAt      time.Time `json:"queried_at"`
}
type cached struct {
	value Location
	until time.Time
	err   error
}
type source struct {
	name, endpoint string
	interval       time.Duration
	v4only         bool
	mu             sync.Mutex
	next, blocked  time.Time
	failures       int
}
type Service struct {
	HTTP    *http.Client
	config  config.Config
	log     *slog.Logger
	local   *maxminddb.Reader
	sources []*source
	mu      sync.Mutex
	cache   map[string]cached
	flight  singleflight.Group
}

func New(c config.Config, l *slog.Logger) (*Service, error) {
	s := &Service{config: c, log: l, cache: map[string]cached{}, HTTP: &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	s.sources = []*source{{name: "ip9", endpoint: "https://ip9.com.cn/get?ip=", interval: 1200 * time.Millisecond}, {name: "ipyou", endpoint: "https://ipyou.net/", interval: 2 * time.Second}, {name: "i5p", endpoint: "https://www.i5p.com/api/ip?ip=", interval: 3 * time.Second}, {name: "pconline", endpoint: "https://whois.pconline.com.cn/ipJson.jsp?json=true&ip=", interval: 2 * time.Second, v4only: true}}
	if c.GeoMode == "local" {
		r, e := maxminddb.Open(c.GeoPath)
		if e != nil {
			return nil, e
		}
		s.local = r
	}
	return s, nil
}
func (s *Service) Close() {
	if s.local != nil {
		s.local.Close()
	}
}

var reserved = []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/96", "64:ff9b::/96", "100::/64", "2001:db8::/32", "2001::/32", "2002::/16", "fc00::/7", "fe80::/10", "ff00::/8"}

func PublicIP(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return false
	}
	for _, p := range reserved {
		if netip.MustParsePrefix(p).Contains(a) {
			return false
		}
	}
	return true
}
func (s *Service) Lookup(ctx context.Context, ip string) (Location, error) {
	a, e := netip.ParseAddr(ip)
	if e != nil {
		return Location{}, e
	}
	a = a.Unmap()
	ip = a.String()
	if !PublicIP(a) || s.config.GeoMode == "disabled" {
		return Location{Source: "unavailable", QueriedAt: time.Now().UTC()}, nil
	}
	s.mu.Lock()
	v, ok := s.cache[ip]
	s.mu.Unlock()
	if ok && v.until.After(time.Now()) {
		return v.value, v.err
	}
	ch := s.flight.DoChan(ip, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		var out Location
		var err error
		if s.local != nil {
			out, err = s.lookupLocal(a)
			if err == nil && out.Country != "" {
				return s.remember(ip, out, nil), nil
			}
			if !s.config.GeoOnlineFallback {
				return s.remember(ip, out, err), err
			}
		}
		for _, src := range s.sources {
			if src.v4only && !a.Is4() {
				continue
			}
			out, err = s.lookupOnline(ctx, src, ip)
			if err == nil {
				return s.remember(ip, out, nil), nil
			}
		}
		out = Location{Source: "unknown", QueriedAt: time.Now().UTC()}
		s.remember(ip, out, ErrUnavailable)
		return out, ErrUnavailable
	})
	select {
	case <-ctx.Done():
		return Location{}, ctx.Err()
	case r := <-ch:
		if r.Val == nil {
			return Location{}, r.Err
		}
		return r.Val.(Location), r.Err
	}
}
func (s *Service) remember(ip string, p Location, e error) Location {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= 4096 {
		for k, v := range s.cache {
			if v.until.Before(time.Now()) {
				delete(s.cache, k)
			}
		}
		if len(s.cache) >= 4096 {
			for k := range s.cache {
				delete(s.cache, k)
				break
			}
		}
	}
	ttl := 24 * time.Hour
	if e != nil || p.Country == "" {
		ttl = 5 * time.Minute
	}
	s.cache[ip] = cached{p, time.Now().Add(ttl), e}
	return p
}
func (s *Service) lookupOnline(ctx context.Context, src *source, ip string) (out Location, err error) {
	src.mu.Lock()
	defer src.mu.Unlock()
	now := time.Now()
	if src.blocked.After(now) {
		return out, ErrUnavailable
	}
	if d := time.Until(src.next); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-t.C:
		}
	}
	src.next = time.Now().Add(src.interval)
	defer func() {
		if err != nil {
			src.failures++
			if src.failures >= 3 {
				delay := time.Minute * time.Duration(min(src.failures-2, 10))
				if time.Until(src.blocked) < delay {
					src.blocked = time.Now().Add(delay)
				}
			}
		} else {
			src.failures = 0
			src.blocked = time.Time{}
		}
	}()
	req, e := http.NewRequestWithContext(ctx, "GET", src.endpoint+url.QueryEscape(ip), nil)
	if e != nil {
		return out, e
	}
	req.Header.Set("Accept", "application/json")
	r, e := s.HTTP.Do(req)
	if e != nil {
		return out, ErrUnavailable
	}
	defer r.Body.Close()
	if r.StatusCode == 429 {
		if n, e := strconv.Atoi(r.Header.Get("Retry-After")); e == nil {
			src.blocked = time.Now().Add(time.Duration(min(max(n, 1), 600)) * time.Second)
		} else if at, e := http.ParseTime(r.Header.Get("Retry-After")); e == nil {
			src.blocked = at
		}
		return out, ErrUnavailable
	}
	if r.StatusCode != 200 {
		return out, ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 65537))
	if e != nil || len(b) > 65536 {
		return out, ErrUnavailable
	}
	if src.name == "pconline" {
		b, _, e = transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), b)
		if e != nil {
			return out, e
		}
	}
	out, e = parse(src.name, b, ip)
	return out, e
}
func parse(name string, b []byte, ip string) (Location, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(b, &root) != nil {
		return Location{}, ErrUnavailable
	}
	obj := root
	if name == "ip9" || name == "i5p" {
		code := "200"
		key := "ret"
		if name == "i5p" {
			code = "0"
			key = "code"
		}
		if strings.Trim(string(root[key]), `"`) != code {
			return Location{}, ErrUnavailable
		}
		if json.Unmarshal(root["data"], &obj) != nil {
			return Location{}, ErrUnavailable
		}
	}
	str := func(m map[string]json.RawMessage, k string) string {
		var v string
		_ = json.Unmarshal(m[k], &v)
		if len(v) > 160 {
			return ""
		}
		return strings.TrimSpace(v)
	}
	a, e := netip.ParseAddr(str(obj, "ip"))
	want, _ := netip.ParseAddr(ip)
	if e != nil || a.Unmap() != want.Unmap() {
		return Location{}, ErrUnavailable
	}
	if name == "ipyou" {
		if json.Unmarshal(root["geo"], &obj) != nil {
			return Location{}, ErrUnavailable
		}
	}
	p := Location{Country: str(obj, "country"), Province: str(obj, "province"), City: str(obj, "city"), District: str(obj, "district"), ISP: str(obj, "isp"), ASN: str(obj, "asn"), Source: name, QueriedAt: time.Now().UTC()}
	if name == "ip9" {
		p.Province = str(obj, "prov")
		p.District = str(obj, "area")
	}
	if name == "pconline" {
		if str(obj, "err") != "" {
			return p, ErrUnavailable
		}
		p.Province = str(obj, "pro")
		p.City = str(obj, "city")
		p.District = str(obj, "region")
		if p.Province != "" {
			p.Country = "中国"
		}
	}
	number := func(keys ...string) *float64 {
		for _, k := range keys {
			v := strings.Trim(string(obj[k]), `"`)
			if n, e := strconv.ParseFloat(v, 64); e == nil {
				return &n
			}
		}
		return nil
	}
	p.Latitude = number("latitude", "lat")
	p.Longitude = number("longitude", "lng")
	if p.Country == "" {
		return p, ErrUnavailable
	}
	return p, nil
}
func (s *Service) lookupLocal(ip netip.Addr) (Location, error) {
	var d struct {
		Country struct {
			Names map[string]string `maxminddb:"names"`
		} `maxminddb:"country"`
		Subdivisions []struct {
			Names map[string]string `maxminddb:"names"`
		} `maxminddb:"subdivisions"`
		City struct {
			Names map[string]string `maxminddb:"names"`
		} `maxminddb:"city"`
		Location struct {
			Latitude  *float64 `maxminddb:"latitude"`
			Longitude *float64 `maxminddb:"longitude"`
			Accuracy  *uint16  `maxminddb:"accuracy_radius"`
		} `maxminddb:"location"`
		ISP          string `maxminddb:"isp"`
		Organization string `maxminddb:"autonomous_system_organization"`
		ASN          uint   `maxminddb:"autonomous_system_number"`
	}
	r := s.local.Lookup(ip)
	if !r.Found() {
		return Location{}, nil
	}
	if e := r.Decode(&d); e != nil {
		return Location{}, e
	}
	name := func(m map[string]string) string {
		if m["zh-CN"] != "" {
			return m["zh-CN"]
		}
		return m["en"]
	}
	p := Location{Country: name(d.Country.Names), City: name(d.City.Names), ISP: d.ISP, Source: "mmdb", Version: s.local.Metadata.BuildTime().UTC().Format(time.RFC3339), QueriedAt: time.Now().UTC(), Latitude: d.Location.Latitude, Longitude: d.Location.Longitude}
	if d.Location.Accuracy != nil {
		radius := float64(*d.Location.Accuracy)
		p.AccuracyRadius = &radius
	}
	if len(d.Subdivisions) > 0 {
		p.Province = name(d.Subdivisions[0].Names)
	}
	if len(d.Subdivisions) > 1 {
		p.District = name(d.Subdivisions[1].Names)
	}
	if d.ASN > 0 {
		p.ASN = "AS" + strconv.FormatUint(uint64(d.ASN), 10)
	}
	if p.ISP == "" {
		p.ISP = d.Organization
	}
	return p, nil
}
