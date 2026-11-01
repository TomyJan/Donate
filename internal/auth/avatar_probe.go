package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	_ "golang.org/x/image/webp"
)

const avatarProbeTimeout = 4 * time.Second
const avatarMaxBytes = 1024 * 1024

var errAvatarAddress = errors.New("avatar address is not public")

var avatarReserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

func publicAvatarAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.Zone() != "" {
		return false
	}
	for _, prefix := range avatarReserved {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

type avatarDialer struct {
	lookup func(context.Context, string) ([]netip.Addr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func (d avatarDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errAvatarAddress
	}
	ips, err := d.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errAvatarAddress
	}
	// Reject mixed public/private answers, then connect to the checked IP itself.
	// A second DNS lookup would reintroduce rebinding between validation and dial.
	for _, ip := range ips {
		if !publicAvatarAddress(ip) {
			return nil, errAvatarAddress
		}
	}
	for _, ip := range ips {
		var conn net.Conn
		conn, err = d.dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, err
}

func newAvatarProbeClient() *http.Client {
	d := avatarDialer{
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		dial: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
	}
	// This client never inherits OAuth transports, credentials, cookies or proxy
	// settings. TLS still verifies the original hostname while the dial pins IPs.
	return &http.Client{
		Timeout:       avatarProbeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DialContext:            d.DialContext,
			TLSHandshakeTimeout:    2 * time.Second,
			ResponseHeaderTimeout:  2 * time.Second,
			MaxResponseHeaderBytes: 32 * 1024,
			DisableKeepAlives:      true,
		},
	}
}

func avatarProbeReason(err error, fallback string) string {
	var timeout net.Error
	var certificate *tls.CertificateVerificationError
	switch {
	case errors.Is(err, errAvatarAddress):
		return "local_address"
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout():
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &certificate):
		return "tls_error"
	default:
		return fallback
	}
}

func probeHTTPSAvatar(ctx context.Context, client *http.Client, raw string) (string, int) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "invalid_request", 0
	}
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp")
	// Even a substituted client must not follow a redirect or attach cookies.
	c := *client
	c.Jar = nil
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, err := c.Do(req)
	if err != nil {
		return avatarProbeReason(err, "request_failed"), 0
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return "http_status", r.StatusCode
	}
	if r.ContentLength > avatarMaxBytes {
		return "response_too_large", r.StatusCode
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, avatarMaxBytes+1))
	if err != nil {
		return avatarProbeReason(err, "response_read"), r.StatusCode
	}
	if len(body) > avatarMaxBytes {
		return "response_too_large", r.StatusCode
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return "image_decode_failed", r.StatusCode
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
		return "image_dimensions_rejected", r.StatusCode
	}
	if _, _, err := image.Decode(bytes.NewReader(body)); err != nil {
		return "image_decode_failed", r.StatusCode
	}
	return "", r.StatusCode
}
