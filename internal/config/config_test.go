package config

import "testing"

func TestLoginProviderOptionsValidatePublicIdentifiers(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"OIDC_ID", "qq"}, {"OIDC_ID", "../oidc"}, {"OIDC_ID", "a/b"}, {"OIDC_ID", "Upper"},
		{"OIDC_ICON_URL", "javascript:alert(1)"}, {"OIDC_ICON_URL", "http://icons.test/logo.svg"},
		{"OIDC_ICON_URL", "https://user:pass@icons.test/logo.svg"}, {"QQ_USE_UNIONID", "enabled"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv("APP_ENV", "development")
			t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("unsafe or ambiguous option accepted")
			}
		})
	}
}

func TestLoadRejectsInvalidProductionURL(t *testing.T) {
	t.Setenv("PUBLIC_URL", "http://donate.example")
	if _, err := Load(); err == nil {
		t.Fatal("insecure production URL accepted")
	}
}

func TestLoadDevelopmentAndAdminIDs(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
	t.Setenv("ADMIN_USER_IDS", "invalid")
	if _, err := Load(); err == nil {
		t.Fatal("invalid admin ID accepted")
	}
}
