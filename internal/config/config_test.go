package config

import "testing"

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
