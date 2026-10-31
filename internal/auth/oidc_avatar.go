package auth

import (
	"encoding/json"
	"net/netip"
	"net/url"
	"strings"
)

type oidcProfileClaims struct {
	Name      string          `json:"name"`
	Preferred string          `json:"preferred_username"`
	Picture   string          `json:"picture"`
	Avatar    json.RawMessage `json:"avatar"`
}

type oidcAvatarClaim struct {
	source, value string
	invalid       bool
}

func (p oidcProfileClaims) avatarClaim(source string) oidcAvatarClaim {
	var value string
	invalid := len(p.Avatar) > 0 && json.Unmarshal(p.Avatar, &value) != nil
	return oidcAvatarClaim{source + ".avatar", value, invalid}
}

// Do not resolve relative URLs or upgrade HTTP to HTTPS: either may point at a
// different resource. Explicit local addresses are not suitable browser avatars.
func safeOIDCAvatar(raw string) (string, string) {
	if raw == "" {
		return "", ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "invalid_url"
	}
	if u.Scheme != "https" || u.Hostname() == "" {
		return "", "https_required"
	}
	if u.User != nil {
		return "", "credentials"
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	lastLabel := host[strings.LastIndex(host, ".")+1:]
	numericLastLabel := lastLabel != "" && strings.Trim(lastLabel, "0123456789") == ""
	hexLastLabel := strings.HasPrefix(lastLabel, "0x") && len(lastLabel) > 2 && strings.Trim(lastLabel[2:], "0123456789abcdef") == ""
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return "", "local_address"
		}
	} else if numericLastLabel || hexLastLabel || !strings.Contains(host, ".") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		// Browsers also recognize non-canonical IPv4 such as 127.1 or
		// 0x7f.0.0.1. Never treat these as ordinary public DNS names.
		return "", "local_address"
	}
	return SafeAvatar(raw), ""
}

func selectOIDCAvatar(candidates ...oidcAvatarClaim) (avatar, source, rejected string) {
	var reasons []string
	for _, candidate := range candidates {
		reason := "invalid_type"
		if !candidate.invalid {
			avatar, reason = safeOIDCAvatar(candidate.value)
		}
		if avatar != "" {
			return avatar, candidate.source, strings.Join(reasons, ",")
		}
		if reason != "" {
			reasons = append(reasons, candidate.source+":"+reason)
		}
	}
	return "", "none", strings.Join(reasons, ",")
}
