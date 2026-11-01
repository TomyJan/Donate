package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadRejectsUnserializablePublicOrigins(t *testing.T) {
	completeConfig(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_SECRET", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	for _, value := range []string{
		"https://example.com/path", "https://example.com?x=1", "https://example.com?", "https://example.com#x", "https://example.com#",
		"https://user@example.com", "https://:443", "https://example.com:65536", "https://[fe80::1%25eth0]", "https://[example.com]",
		"https://127.1", "https://0x7f000001", "https://0177.0.0.1", "https://2130706433", "https://example.123",
		"https://example.com|evil", "https://example.com^evil", "http://example.com",
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PUBLIC_URL", value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PUBLIC_URL") {
				t.Fatalf("unsupported browser origin %q accepted: %v", value, err)
			}
		})
	}
}

func TestPublicOriginPreservesExactCallbackConfiguration(t *testing.T) {
	c := Config{PublicURL: "https://BÜCHER.example:00443", OIDCissuer: "https://SSO.example:443/oauth", EpayURL: "https://EPAY.example:443/pay"}
	originalPublic, originalOIDC, originalEPay := c.PublicURL, c.OIDCissuer, c.EpayURL
	origin, err := c.PublicOrigin()
	if err != nil || origin != "https://xn--bcher-kva.example" {
		t.Fatalf("browser origin %q: %v", origin, err)
	}
	if c.PublicURL != originalPublic || c.OIDCissuer != originalOIDC || c.EpayURL != originalEPay {
		t.Fatal("origin comparison rewrote registered callback configuration")
	}
}
