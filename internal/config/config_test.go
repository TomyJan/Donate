package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadRejectsInvalidUmamiConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "production")
	t.Setenv("PUBLIC_URL", "https://donate.example.com")
	t.Setenv("APP_SECRET", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	const websiteID = "a4dca193-c108-47d7-a429-b89c13c7f747"
	for _, tc := range []struct{ name, script, id string }{
		{"missing website", "https://stats.example.com/script.js", ""},
		{"missing script", "", websiteID},
		{"invalid website", "https://stats.example.com/script.js", "not-a-website-id"},
		{"HTTP in production", "http://stats.example.com/script.js", websiteID},
		{"executable URL", "javascript:alert(1)", websiteID},
		{"protocol relative", "//stats.example.com/script.js", websiteID},
		{"credentials", "https://user:secret@stats.example.com/script.js", websiteID},
		{"fragment", "https://stats.example.com/script.js#tracker", websiteID},
		{"CSP wildcard", "https://*.example.com/script.js", websiteID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("UMAMI_SCRIPT_URL", tc.script)
			t.Setenv("UMAMI_WEBSITE_ID", tc.id)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "UMAMI_") {
				t.Fatalf("expected explicit Umami configuration error, got %v", err)
			}
		})
	}
}

func TestLoadUmamiSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_SECRET", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	const websiteID = "a4dca193-c108-47d7-a429-b89c13c7f747"
	for _, tc := range []struct {
		name, environment, script, id, origin string
	}{
		{"disabled", "production", "", "", ""},
		{"HTTPS endpoint", "production", "https://stats.example.com/tracker/script.js?v=1&site=donate", websiteID, "https://stats.example.com"},
		{"custom port", "production", "https://stats.example.com:8443/script.js", websiteID, "https://stats.example.com:8443"},
		{"development", "development", "http://127.0.0.1:3000/script.js", websiteID, "http://127.0.0.1:3000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.environment)
			t.Setenv("PUBLIC_URL", "https://donate.example.com")
			t.Setenv("UMAMI_SCRIPT_URL", tc.script)
			t.Setenv("UMAMI_WEBSITE_ID", tc.id)
			c, err := Load()
			if err != nil || c.UmamiScriptURL != tc.script || c.UmamiWebsiteID != tc.id || c.UmamiOrigin != tc.origin {
				t.Fatalf("Umami settings = (%q, %q, %q), error = %v", c.UmamiScriptURL, c.UmamiWebsiteID, c.UmamiOrigin, err)
			}
		})
	}
}

func TestLoadAcceptsEmptyAdminIDsWithInlineComment(t *testing.T) {
	for _, value := range []string{"", "   ", "   # 登录后再填写", "\"\" # 登录后再填写"} {
		t.Run(value, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("APP_ENV", "development")
			t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
			t.Setenv("ADMIN_USER_IDS", "")
			if err := os.Unsetenv("ADMIN_USER_IDS"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(".env", []byte("ADMIN_USER_IDS="+value+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load()
			if err != nil || len(c.AdminIDs) != 0 {
				t.Fatalf("empty admin list must allow bootstrap: admins=%d error=%v", len(c.AdminIDs), err)
			}
		})
	}
}

func TestExampleEnvironmentBootsWithEmptyOptionalSettings(t *testing.T) {
	contents, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for _, line := range strings.Split(string(contents), "\n") {
		key, _, ok := strings.Cut(line, "=")
		if ok {
			t.Setenv(strings.TrimSpace(key), "")
			if err := os.Unsetenv(strings.TrimSpace(key)); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("APP_SECRET", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err := os.WriteFile(".env", contents, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AdminIDs) != 0 || c.OIDCissuer != "" || c.QQappID != "" || c.EpayURL != "" || c.RealIPHeader != "" {
		t.Fatal("empty optional settings became non-empty")
	}
}

func TestLoadPreservesQuotedHashesAndMultilineValues(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
	for _, key := range []string{"ADMIN_USER_IDS", "OIDC_NAME", "EPAY_PRIVATE_KEY"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	contents := "ADMIN_USER_IDS= # optional\nOIDC_NAME=\"Campus # login\" # comment\nEPAY_PRIVATE_KEY=\"first\nINSIDE= # literal content\nlast\"\n"
	if err := os.WriteFile(".env", []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.OIDCname != "Campus # login" || c.EpayPrivateKey != "first\nINSIDE= # literal content\nlast" {
		t.Fatal("quoted environment values were modified")
	}
}

func TestParseEnvPreservesLiteralHashAndYAMLMultilineValues(t *testing.T) {
	newline, quote := string(rune(10)), string(rune(34))
	for _, tc := range []struct{ name, input, key, want string }{
		{"literal hash", "OIDC_CLIENT_SECRET=#literal", "OIDC_CLIENT_SECRET", "#literal"},
		{"YAML multiline", strings.Join([]string{"OIDC_NAME: " + quote + "first", "INSIDE= # literal", "last" + quote}, newline), "OIDC_NAME", strings.Join([]string{"first", "INSIDE= # literal", "last"}, newline)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values, err := parseEnv([]byte(tc.input))
			if err != nil || values[tc.key] != tc.want {
				t.Fatalf("value = %q, error = %v", values[tc.key], err)
			}
		})
	}
}

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

func TestRealIPHeaderConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
	for _, tc := range []struct {
		value, want string
		invalid     bool
	}{
		{"", "", false},
		{"x-real-ip", "X-Real-Ip", false},
		{"X-Forwarded-For", "X-Forwarded-For", false},
		{"CF-Connecting-IP", "Cf-Connecting-Ip", false},
		{"X Real IP", "", true},
		{"X-Real-IP,X-Forwarded-For", "", true},
		{"X-Real-IP: value", "", true},
		{"X-Real-IP\r\nInjected", "", true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("REAL_IP_HEADER", tc.value)
			c, err := Load()
			if tc.invalid {
				if err == nil || !strings.Contains(err.Error(), "REAL_IP_HEADER") {
					t.Fatalf("expected a header-name error, got %v", err)
				}
				return
			}
			if err != nil || c.RealIPHeader != tc.want {
				t.Fatalf("header = %q, error = %v, want %q", c.RealIPHeader, err, tc.want)
			}
		})
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
