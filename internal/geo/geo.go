package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/observability"
	"github.com/oschwald/maxminddb-golang/v2"
	"golang.org/x/sync/singleflight"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var ErrUnavailable = errors.New("属地查询暂不可用")
var ErrDisabled = errors.New("属地查询已禁用")
var ErrIncomplete = errors.New("属地查询未取得省级信息")
var ErrConflict = errors.New("属地来源存在未解决的分歧")

type Location struct {
	Country        string     `json:"country"`
	Province       string     `json:"province"`
	City           string     `json:"city"`
	District       string     `json:"district"`
	Street         string     `json:"street"`
	ISP            string     `json:"isp"`
	ASN            string     `json:"asn"`
	Latitude       *float64   `json:"latitude,omitempty"`
	Longitude      *float64   `json:"longitude,omitempty"`
	AccuracyRadius *float64   `json:"accuracy_radius,omitempty"`
	Source         string     `json:"source"`
	Version        string     `json:"version"`
	QueriedAt      time.Time  `json:"queried_at"`
	Verification   string     `json:"verification,omitempty"`
	Conflict       bool       `json:"conflict,omitempty"`
	Evidence       []Evidence `json:"evidence,omitempty"`
}
type cached struct {
	value    Location
	until    time.Time
	err      error
	revision uint64
}
type source struct {
	name, endpoint string
	interval       time.Duration
	v4only         bool
	mu             sync.Mutex
	gate           chan struct{}
	next, blocked  time.Time
	failures       int
}
type Service struct {
	HTTP       *http.Client
	config     config.Config
	log        *slog.Logger
	local      *maxminddb.Reader
	xdb4, xdb6 *xdbReader
	localMu    sync.RWMutex
	closed     bool
	sources    []*source
	mu         sync.Mutex
	cache      map[string]cached
	flight     singleflight.Group
	revision   uint64
	generation uint64
}

func New(c config.Config, l *slog.Logger) (*Service, error) {
	if l == nil {
		l = slog.Default()
	}
	s := &Service{config: c, log: l, cache: map[string]cached{}, HTTP: &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	s.sources = []*source{{name: "ip9", endpoint: "https://ip9.com.cn/get?ip=", interval: 1200 * time.Millisecond}, {name: "ipyou", endpoint: "https://ipyou.net/", interval: 2 * time.Second}, {name: "i5p", endpoint: "https://www.i5p.com/api/ip?ip=", interval: 3 * time.Second}, {name: "pconline", endpoint: "https://whois.pconline.com.cn/ipJson.jsp?json=true&ip=", interval: 2 * time.Second, v4only: true}}
	if c.GeoMode == "local" {
		s.openLocal()
	}
	return s, nil
}
func (s *Service) Close() {
	s.localMu.Lock()
	defer s.localMu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.xdb4 != nil {
		s.xdb4.close()
	}
	if s.xdb6 != nil {
		s.xdb6.close()
	}
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
	return s.lookup(ctx, ip, false)
}

// Refresh uses the configured sources and rate limits, but never reuses a cached
// result or an ordinary lookup already in flight.
func (s *Service) Refresh(ctx context.Context, ip string) (Location, error) {
	return s.lookup(ctx, ip, true)
}

func (s *Service) lookup(ctx context.Context, ip string, force bool) (Location, error) {
	s.localMu.RLock()
	closed := s.closed
	s.localMu.RUnlock()
	if closed {
		return Location{}, ErrUnavailable
	}
	a, e := netip.ParseAddr(ip)
	if e != nil {
		return Location{}, e
	}
	a = a.Unmap()
	ip = a.String()
	if force && s.config.GeoMode == "disabled" {
		return Location{}, ErrDisabled
	}
	if !PublicIP(a) || s.config.GeoMode == "disabled" {
		if force {
			return Location{}, ErrUnavailable
		}
		return Location{Source: "unavailable", QueriedAt: time.Now().UTC()}, nil
	}
	s.mu.Lock()
	v, ok := s.cache[ip]
	generation := s.generation
	revision := v.revision
	if !ok {
		revision = s.revision
	}
	s.mu.Unlock()
	if !force && ok && v.until.After(time.Now()) {
		return cloneLocation(v.value), v.err
	}
	flightKey := strconv.FormatUint(generation, 10) + ":" + ip
	if force {
		flightKey = "refresh:" + flightKey
	}
	ch := s.flight.DoChan(flightKey, func() (any, error) {
		lookupRevision := revision
		if force {
			s.mu.Lock()
			entry := s.cache[ip]
			s.revision++
			entry.revision = s.revision
			s.cache[ip] = entry
			lookupRevision = entry.revision
			s.mu.Unlock()
		}
		remember := func(p Location, err error) Location { return s.remember(ip, p, err, lookupRevision, force) }
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		out, err := s.resolve(ctx, a)
		return remember(out, err), err
	})
	select {
	case <-ctx.Done():
		return Location{}, ctx.Err()
	case r := <-ch:
		if r.Val == nil {
			return Location{}, r.Err
		}
		out := cloneLocation(r.Val.(Location))
		if force && r.Err == nil && out.Verification == "conflict" {
			return out, ErrConflict
		}
		if force && r.Err == nil && out.Country == "" {
			return out, ErrUnavailable
		}
		if force && r.Err == nil && strings.TrimSpace(out.Province) == "" {
			return out, ErrIncomplete
		}
		return out, r.Err
	}
}
func (s *Service) remember(ip string, p Location, e error, revision uint64, force bool) Location {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, exists := s.cache[ip]
	// An evicted entry must not let an older lookup recreate a stale cache value.
	if (exists && entry.revision != revision) || (!exists && s.revision != revision) {
		return p
	}
	if force {
		// Fence out ordinary lookups that started while this refresh was in flight.
		s.revision++
		revision = s.revision
	}
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
	if e != nil || p.Country == "" || strings.TrimSpace(p.Province) == "" || (s.config.GeoMode == "local" && s.config.GeoOnlineFallback && p.Verification == "single") {
		ttl = 5 * time.Minute
	}
	s.cache[ip] = cached{value: cloneLocation(p), until: time.Now().Add(ttl), err: e, revision: revision}
	return p
}
func (s *Service) lookupOnline(ctx context.Context, src *source, ip string) (out Location, err error) {
	started := time.Now()
	defer func() {
		if err != nil {
			level := slog.LevelWarn
			var f *sourceFailure
			if errors.Is(err, context.Canceled) || errors.As(err, &f) && f.reason == "circuit_open" {
				level = slog.LevelDebug
			}
			detail := Diagnostic(err)
			prefix, fields := traceFields(ctx)
			fields = append(fields, "ip", ip, "source", src.name, "diagnostic", detail, "duration_ms", time.Since(started).Milliseconds())
			if f != nil {
				fields = append(fields, "stage", f.stage, "reason", f.reason, "http_status", f.status)
			}
			s.log.Log(ctx, level, prefix+fmt.Sprintf("IP %s 的属地来源 %s 查询失败，%s，耗时 %d 毫秒", ip, src.name, detail, time.Since(started).Milliseconds()), fields...)
		} else {
			s.log.DebugContext(ctx, fmt.Sprintf("IP %s 的属地来源 %s 返回国家 %s、省份 %s，耗时 %d 毫秒", ip, src.name, out.Country, out.Province, time.Since(started).Milliseconds()), "ip", ip, "source", src.name, "country", out.Country, "province", out.Province, "duration_ms", time.Since(started).Milliseconds())
		}
	}()
	src.mu.Lock()
	if src.gate == nil {
		src.gate = make(chan struct{}, 1)
	}
	gate := src.gate
	src.mu.Unlock()
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return out, ctx.Err()
	}
	defer func() { <-gate }()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	now := time.Now()
	if src.blocked.After(now) {
		return out, failure("rate_limit", "circuit_open", 0, ErrUnavailable)
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
		return out, failure("request", "request_invalid", 0, e)
	}
	req.Header.Set("Accept", "application/json")
	r, e := s.HTTP.Do(req)
	if e != nil {
		return out, failure("request", observability.ErrorReason(e), 0, e)
	}
	defer r.Body.Close()
	if r.StatusCode == 429 {
		if n, e := strconv.Atoi(r.Header.Get("Retry-After")); e == nil {
			src.blocked = time.Now().Add(time.Duration(min(max(n, 1), 600)) * time.Second)
		} else if at, e := http.ParseTime(r.Header.Get("Retry-After")); e == nil {
			src.blocked = at
		}
		return out, failure("response", "rate_limited", 429, ErrUnavailable)
	}
	if r.StatusCode != 200 {
		return out, failure("response", "http_status", r.StatusCode, ErrUnavailable)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 65537))
	if e != nil {
		return out, failure("body", observability.ErrorReason(e), r.StatusCode, e)
	}
	if len(b) > 65536 {
		return out, failure("body", "response_too_large", r.StatusCode, ErrUnavailable)
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
		return Location{}, failure("decode", "invalid_json", 0, ErrUnavailable)
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
			return Location{}, failure("provider", "provider_rejected", 0, ErrUnavailable)
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
		return Location{}, failure("validate", "ip_mismatch", 0, ErrUnavailable)
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
	if p.Latitude != nil && (math.IsNaN(*p.Latitude) || math.IsInf(*p.Latitude, 0) || *p.Latitude < -90 || *p.Latitude > 90) {
		p.Latitude = nil
	}
	if p.Longitude != nil && (math.IsNaN(*p.Longitude) || math.IsInf(*p.Longitude, 0) || *p.Longitude < -180 || *p.Longitude > 180) {
		p.Longitude = nil
	}
	if p.Country == "" {
		return p, ErrUnavailable
	}
	return normalize(p), nil
}
func (s *Service) lookupMMDB(ip netip.Addr) (Location, error) {
	var d mmdbRecord
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
	return normalize(p), nil
}
