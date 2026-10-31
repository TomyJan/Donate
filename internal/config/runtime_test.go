package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func completeConfig(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, s := range settings {
		t.Setenv(s.key, "")
		if err := os.Unsetenv(s.key); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(".env", Example(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeDefaultsAndEmptyOptionalKeys(t *testing.T) {
	completeConfig(t)
	t.Setenv("APP_ENV", "development")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if c.AppName != "donate" || c.DatabasePath() != filepath.Join(wd, "data", "donate.db") || c.LockPath() != filepath.Join(wd, "data", "donate.lock") || c.LogDir != filepath.Join(wd, "data", "logs") || c.BackupDir != filepath.Join(wd, "data", "backups") || c.LogRetentionDays != 30 || c.BackupRetentionDays != 7 || c.GeoUpdateDays != 7 {
		t.Fatalf("unexpected runtime layout: %+v", c)
	}
	if len(c.AdminIDs) != 0 || len(c.LoginProviders()) != 0 || c.EpayURL != "" {
		t.Fatal("explicitly empty optional keys must remain disabled")
	}
	t.Setenv("APP_NAME", "my-donate")
	c, err = Load()
	if err != nil || filepath.Base(c.DatabasePath()) != "my-donate.db" {
		t.Fatal("process app name did not control filename", c.DatabasePath(), err)
	}
}

func TestRuntimeRejectsInvalidSettingsWithoutLeakingURL(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"APP_NAME", "UPPER"}, {"APP_NAME", "name1"}, {"APP_NAME", "-app"}, {"APP_NAME", "app-"}, {"APP_NAME", "a/b"}, {"APP_NAME", strings.Repeat("a", 64)},
		{"LOG_RETENTION_DAYS", "-1"}, {"BACKUP_RETENTION_DAYS", "oops"}, {"GEO_UPDATE_DAYS", "99999999"},
		{"GEO_ONLINE_FALLBACK", "maybe"}, {"LISTEN_ADDR", "localhost:bad"}, {"LOG_LEVEL", "strange"},
		{"GEO_XDB_IPV4_URL", "file:///private-key"}, {"GEO_DATABASE_URL", "https://example.com/db#private-key"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			completeConfig(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.key) || strings.Contains(err.Error(), "private-key") {
				t.Fatal("invalid runtime setting accepted or disclosed", err)
			}
		})
	}
}

func TestExampleWriteFailureStillRejectsStartup(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir(".env.example", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "无法写出 .env.example") {
		t.Fatal("example failure falsely reported successful output", err)
	}
}

func TestDatabaseDownloadsRequireSeparateTargets(t *testing.T) {
	for _, tc := range []struct{ path, url string }{
		{"GEO_XDB_IPV4", "GEO_XDB_IPV4_URL"},
		{"GEO_XDB_IPV6", "GEO_XDB_IPV6_URL"},
		{"GEO_DATABASE", "GEO_DATABASE_URL"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			completeConfig(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv(tc.path, "")
			t.Setenv(tc.url, "https://example.com/db?key=secret-value")
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.path) || strings.Contains(err.Error(), "secret-value") {
				t.Fatal("download without a target accepted or disclosed", err)
			}
		})
	}
	t.Run("duplicate target", func(t *testing.T) {
		completeConfig(t)
		t.Setenv("APP_ENV", "development")
		t.Setenv("GEO_MODE", "local")
		t.Setenv("GEO_XDB_IPV4", "data/ipdb/shared.xdb")
		t.Setenv("GEO_XDB_IPV6", "data/ipdb/./shared.xdb")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "不能相同") {
			t.Fatal("two protocols accepted the same database target", err)
		}
	})
}

func TestExampleCommentsHaveWideConsistentIndentation(t *testing.T) {
	for _, line := range strings.Split(string(Example()), "\n") {
		if strings.HasPrefix(line, " ") && !strings.HasPrefix(line, strings.Repeat(" ", 48)+"# ") {
			t.Fatalf("configuration comment is not aligned at column 49: %q", line)
		}
	}
}

func TestMissingDotEnvWritesSafeExample(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_SECRET", "do-not-copy-this-secret")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), ".env") {
		t.Fatalf("missing .env must prevent startup: %v", err)
	}
	b, err := os.ReadFile(".env.example")
	if err != nil || !strings.Contains(string(b), "APP_NAME=donate") || strings.Contains(string(b), "do-not-copy-this-secret") {
		t.Fatalf("safe current example was not generated: %s %v", b, err)
	}
	if _, err := os.Stat(".env"); !os.IsNotExist(err) {
		t.Fatal("missing actual configuration was silently created", err)
	}
}

func TestProcessEnvironmentCannotFillMissingDotEnvKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_URL", "https://example.com")
	t.Setenv("APP_NAME", "process-app")
	input := []byte("APP_ENV=development\nPUBLIC_URL=https://example.com\n")
	if err := os.WriteFile(".env", input, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "APP_NAME") || !strings.Contains(err.Error(), "LOG_RETENTION_DAYS") {
		t.Fatal("incomplete file accepted despite missing configuration declarations", err)
	}
	if actual, err := os.ReadFile(".env"); err != nil || string(actual) != string(input) {
		t.Fatal("operator configuration was modified", err)
	}
}
