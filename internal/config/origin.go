package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

var browserHost = idna.New(idna.MapForLookup(), idna.StrictDomainName(false), idna.CheckHyphens(false), idna.CheckJoiners(true), idna.BidiRule(), idna.Transitional(false))

// PublicOrigin serializes the trusted site's browser origin for exact comparison.
// PublicURL itself remains unchanged because registered callbacks use its spelling.
func (c Config) PublicOrigin() (string, error) {
	invalid := errors.New("PUBLIC_URL 必须为无路径、查询、片段或凭据的有效 HTTPS 站点地址；HTTP 仅用于开发")
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.Opaque != "" || strings.ContainsAny(c.PublicURL, "?#") || (u.Scheme != "https" && !(c.Development && u.Scheme == "http")) {
		return "", invalid
	}
	host := u.Hostname()
	if strings.HasPrefix(u.Host, "[") {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is6() || ip.Zone() != "" {
			return "", invalid
		}
		host = ip.String()
		if ip.Is4In6() {
			// Browsers serialize mapped IPv6 with hexadecimal pieces, not a dotted tail.
			b := ip.As16()
			host = fmt.Sprintf("::ffff:%x:%x", uint16(b[12])<<8|uint16(b[13]), uint16(b[14])<<8|uint16(b[15]))
		}
		host = "[" + host + "]"
	} else {
		host, err = browserHost.ToASCII(host)
		if err != nil || host == "" || strings.ContainsAny(host, " #%/:<>?@[\\]^|") || strings.IndexFunc(host, func(r rune) bool { return r <= 0x20 || r == 0x7f }) >= 0 {
			return "", invalid
		}
		host = strings.ToLower(host)
		if ip, err := netip.ParseAddr(strings.TrimSuffix(host, ".")); err == nil && ip.Is4() {
			host = ip.String()
		} else if numericHostSuffix(host) {
			// WHATWG rewrites legacy numeric IPv4 forms (127.1, octal, hex).
			// Require an ordinary IPv4 address instead of accepting a broken origin.
			return "", errors.New("PUBLIC_URL 的 IPv4 地址须使用完整的点分十进制形式，不支持缩写、八进制或十六进制地址")
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return "", invalid
		}
		if !(u.Scheme == "https" && n == 443 || u.Scheme == "http" && n == 80) {
			host += ":" + strconv.FormatUint(n, 10)
		}
	}
	return u.Scheme + "://" + host, nil
}

func numericHostSuffix(host string) bool {
	host = strings.TrimSuffix(host, ".")
	last := host[strings.LastIndexByte(host, '.')+1:]
	digits := "0123456789"
	if strings.HasPrefix(last, "0x") {
		last, digits = last[2:], "0123456789abcdef"
	} else if last == "" {
		return false
	}
	return strings.Trim(last, digits) == ""
}
