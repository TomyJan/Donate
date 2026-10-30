package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadDefaultsToWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
	t.Setenv("DATA_DIR", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != want {
		t.Fatalf("data directory = %q, want working directory %q", c.DataDir, want)
	}
}

func TestLoadReadsWorkingDirectoryEnvWithoutOverridingProcess(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{"APP_ENV", "PUBLIC_URL", "DATA_DIR", "OIDC_NAME"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LOG_LEVEL", "WARN")
	t.Setenv("ADMIN_USER_IDS", "")
	contents := "APP_ENV=development\nPUBLIC_URL=http://127.0.0.1:8080\nDATA_DIR='./state'\nOIDC_NAME=\"Campus # login\" # comment\nLOG_LEVEL=DEBUG\nADMIN_USER_IDS=invalid\n"
	if err := os.WriteFile(".env", []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs("state")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Development || c.PublicURL != "http://127.0.0.1:8080" || c.DataDir != want || c.OIDCname != "Campus # login" {
		t.Fatalf("working-directory .env values were not loaded")
	}
	if c.LogLevel != "WARN" || len(c.AdminIDs) != 0 {
		t.Fatal(".env overrode an existing process variable")
	}
}

func TestLoadRejectsUnreadableOrInvalidEnvWithoutExposingContents(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid", true: "directory"}[directory], func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("APP_ENV", "development")
			t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
			var err error
			if directory {
				err = os.Mkdir(".env", 0700)
			} else {
				err = os.WriteFile(".env", []byte("APP_SECRET=\"private-secret-value"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Load(); err == nil || !strings.Contains(err.Error(), ".env") || strings.Contains(err.Error(), "private-secret-value") {
				t.Fatalf("expected a safe .env error, got %v", err)
			}
		})
	}
}

func TestLoadPreservesWindowsProcessEnvironmentCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows environment variables are case-insensitive")
	}
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_URL", "")
	if err := os.Unsetenv("PUBLIC_URL"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("public_url", "https://process.example")
	if err := os.WriteFile(".env", []byte("PUBLIC_URL=https://file.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL != "https://process.example" {
		t.Fatalf(".env overrode a Windows process variable: %s", c.PublicURL)
	}
}

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
