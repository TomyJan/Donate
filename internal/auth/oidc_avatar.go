package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/idna"
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

// Validate HTTPS URLs without fetching. HTTP candidates must separately pass an
// HTTPS image probe; relative URLs and local hosts remain unsuitable avatars.
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
	hostname := u.Hostname()
	host := hostname
	if _, err := netip.ParseAddr(host); err != nil {
		// Browser host parsing applies IDNA mappings before recognizing IPs or
		// local suffixes. Check that same ASCII name, including width/dot and
		// ignored-character mappings; valid IPv6 literals need no IDNA mapping.
		host, err = idna.Lookup.ToASCII(host)
		if err != nil {
			return "", "invalid_host"
		}
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
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
	// Persist the ASCII host that passed validation; retain the path/query,
	// optional port, and IPv6 brackets from the parsed HTTPS URL.
	u.Host = strings.Replace(u.Host, hostname, host, 1)
	return SafeAvatar(u.String()), ""
}

func (s *Service) selectOIDCAvatar(ctx context.Context, candidates ...oidcAvatarClaim) (avatar, source, rejected string) {
	ctx, cancel := context.WithTimeout(ctx, avatarProbeTimeout)
	defer cancel()
	client := s.avatarHTTP
	checked := map[string]string{}
	var reasons []string
	for _, candidate := range candidates {
		reason := "invalid_type"
		if !candidate.invalid {
			raw := candidate.value
			u, err := url.Parse(raw)
			upgrade := err == nil && u.Scheme == "http"
			if upgrade {
				u.Scheme = "https"
				raw = u.String()
			}
			avatar, reason = safeOIDCAvatar(raw)
			if upgrade && avatar != "" {
				var known bool
				reason, known = checked[avatar]
				if !known {
					if client == nil {
						client = newAvatarProbeClient()
					}
					started := time.Now()
					var status int
					reason, status = probeHTTPSAvatar(ctx, client, avatar)
					checked[avatar] = reason
					level, outcome := slog.LevelInfo, "accepted"
					if reason != "" {
						level, outcome = slog.LevelWarn, "rejected"
					}
					_, host := avatarDiagnostics(avatar)
					s.logAuth(ctx, level, s.Config.OIDCProviderID(), "oidc_avatar_upgrade", outcome, started,
						"avatar_source", candidate.source, "avatar_host", host, "reason", reason, "http_status", status)
				}
				if reason != "" {
					avatar, reason = "", "https_upgrade_"+reason
				}
			}
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
