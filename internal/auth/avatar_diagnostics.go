package auth

import "net/url"

// Only a validated public host is useful in diagnostics. Never log the provider's
// complete URL: paths, credentials, fragments and query strings may be private.
func avatarDiagnostics(raw string) (status, host string) {
	if raw == "" {
		return "missing", ""
	}
	safe, _ := safeOIDCAvatar(raw)
	if safe == "" {
		return "rejected", ""
	}
	u, err := url.Parse(safe)
	if err != nil {
		return "rejected", ""
	}
	return "present", u.Hostname()
}

func (s *Service) avatarLogFields(session Session) []any {
	provider := session.Provider
	if provider == "oidc" {
		provider = s.Config.OIDCProviderID()
	}
	status, host := avatarDiagnostics(session.AvatarURL)
	return []any{
		"user_id", session.UserID, "identity_id", session.IdentityID,
		"provider_id", provider, "avatar_source", "stored_identity",
		"avatar_present", session.AvatarURL != "", "avatar_status", status, "avatar_host", host,
	}
}
