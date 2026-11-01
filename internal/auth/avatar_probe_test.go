package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestAvatarDialerRejectsNonPublicDNS(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.0.1", "169.254.169.254", "100.64.0.1", "0.0.0.1", "192.0.0.1", "198.18.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "240.0.0.1", "224.0.0.1", "::1", "::", "fc00::1", "fe80::1", "fec0::1", "feff::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::1", "2001:db8::1"} {
		t.Run(address, func(t *testing.T) {
			calls := 0
			d := avatarDialer{
				lookup: func(context.Context, string) ([]netip.Addr, error) {
					return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr(address)}, nil
				},
				dial: func(context.Context, string, string) (net.Conn, error) {
					calls++
					return nil, errors.New("must not dial")
				},
			}
			if _, err := d.DialContext(context.Background(), "tcp", "public.example:443"); !errors.Is(err, errAvatarAddress) || calls != 0 {
				t.Fatalf("unsafe DNS answer dialed: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestAvatarDialerPinsPublicDNS(t *testing.T) {
	var addresses []string
	lookups := 0
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	d := avatarDialer{
		lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			lookups++
			if host != "public.example" {
				t.Fatalf("unexpected lookup %s", host)
			}
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700:4700::1111")}, nil
		},
		dial: func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" {
				t.Fatalf("unexpected network %s", network)
			}
			addresses = append(addresses, address)
			if len(addresses) == 1 {
				return nil, errors.New("first address unavailable")
			}
			return a, nil
		},
	}
	conn, err := d.DialContext(context.Background(), "tcp", "public.example:443")
	if err != nil || conn != a || lookups != 1 || strings.Join(addresses, ",") != "8.8.8.8:443,[2606:4700:4700::1111]:443" {
		t.Fatalf("DNS must be resolved once then pinned: %v %d %v", err, lookups, addresses)
	}
}

func TestAvatarProbeClientRejectsRedirectsAndInvalidTLS(t *testing.T) {
	hits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/image", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(avatarPNG(t))
	}))
	defer server.Close()
	client := newAvatarProbeClient()
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.TLSClientConfig != nil || client.Jar != nil {
		t.Fatal("avatar probe must not inherit proxy, cookies or weakened TLS settings")
	}
	// Only the test dial target is substituted; retain the real TLS handshake and
	// trust checks against example.com, which is covered by httptest's certificate.
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	if reason, _ := probeHTTPSAvatar(context.Background(), client, "https://example.com/image"); reason != "tls_error" || hits != 0 {
		t.Fatalf("untrusted TLS accepted: reason=%s hits=%d", reason, hits)
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	if reason, status := probeHTTPSAvatar(context.Background(), client, "https://example.com/redirect"); reason != "http_status" || status != 302 || hits != 1 {
		t.Fatalf("redirect followed: reason=%s status=%d hits=%d", reason, status, hits)
	}
	if reason, status := probeHTTPSAvatar(context.Background(), client, "https://example.com/image"); reason != "" || status != 200 || hits != 2 {
		t.Fatalf("verified HTTPS image rejected: reason=%s status=%d hits=%d", reason, status, hits)
	}
	if reason, _ := probeHTTPSAvatar(context.Background(), client, "https://wrong-name.example/image"); reason != "tls_error" || hits != 2 {
		t.Fatalf("hostname verification bypassed: reason=%s hits=%d", reason, hits)
	}
}

func TestAvatarProbeValidatesWholeImageAndDimensions(t *testing.T) {
	var large bytes.Buffer
	if err := jpeg.Encode(&large, image.NewGray(image.Rect(0, 0, 2049, 1)), nil); err != nil {
		t.Fatal(err)
	}
	valid := avatarPNG(t)
	for _, tt := range []struct {
		name   string
		data   []byte
		reason string
	}{
		{"valid PNG", valid, ""},
		{"truncated PNG after valid header", valid[:len(valid)/2], "image_decode_failed"},
		{"dimension limit", large.Bytes(), "image_dimensions_rejected"},
		{"size limit", bytes.Repeat([]byte("x"), avatarMaxBytes+1), "response_too_large"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: qqTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(tt.data))}, nil
			})}
			if reason, _ := probeHTTPSAvatar(context.Background(), client, "https://example.com/image"); reason != tt.reason {
				t.Fatalf("reason=%s want=%s", reason, tt.reason)
			}
		})
	}
}

func TestOIDCAvatarUpgradeKeepsHTTPSAndRejectsLocalURLsWithoutProbing(t *testing.T) {
	s := testAuth(t)
	s.avatarHTTP = &http.Client{Transport: qqTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected probe: %s", r.URL)
		return nil, errors.New("unexpected probe")
	})}
	const fallback = "https://public.example/avatar.png"
	for _, raw := range []string{"http://127.0.0.1/a", "http://127.1/a", "http://10.0.0.1/a", "http://[::1]/a", "http://localhost/a", "http://１２７.０.０.１/a", "http://user:password@public.example/a", "//public.example/a", "/avatar.png"} {
		avatar, _, _ := s.selectOIDCAvatar(context.Background(), oidcAvatarClaim{source: "id_token.picture", value: raw}, oidcAvatarClaim{source: "id_token.avatar", value: fallback})
		if avatar != fallback {
			t.Fatalf("unsafe URL poisoned fallback: %s => %s", raw, avatar)
		}
	}
}

func TestAvatarUpgradeSharesOneDeadlineAcrossCandidates(t *testing.T) {
	s := testAuth(t)
	var first time.Time
	s.avatarHTTP = &http.Client{Transport: qqTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > avatarProbeTimeout {
			t.Error("unbounded probe")
		}
		if first.IsZero() {
			first = deadline
		} else if !deadline.Equal(first) {
			t.Error("each candidate received a fresh timeout")
		}
		return nil, context.DeadlineExceeded
	})}
	avatar, _, _ := s.selectOIDCAvatar(context.Background(), oidcAvatarClaim{source: "id_token.picture", value: "http://public.example/a"}, oidcAvatarClaim{source: "id_token.avatar", value: "http://public.example/b"}, oidcAvatarClaim{source: "userinfo.avatar", value: "https://public.example/c"})
	if avatar != "https://public.example/c" {
		t.Fatalf("timeout lost safe fallback: %q", avatar)
	}
}
