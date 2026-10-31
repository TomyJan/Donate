package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
)

func TestStartupWithoutConfigGeneratesExampleAndStops(t *testing.T) {
	t.Chdir(t.TempDir())
	previous := os.Args
	os.Args = []string{"donate"}
	t.Cleanup(func() { os.Args = previous })
	if err := run(); err == nil || !strings.Contains(err.Error(), "缺少 .env") {
		t.Fatal("startup accepted missing configuration", err)
	}
	if data, err := os.ReadFile(".env.example"); err != nil || string(data) != string(config.Example()) {
		t.Fatal("current example not written", err)
	}
	if _, err := os.Stat("data"); !os.IsNotExist(err) {
		t.Fatal("failed startup created data files", err)
	}
}

func TestVersionDoesNotRequireOrCreateConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	previous := os.Args
	os.Args = []string{"donate", "--version"}
	t.Cleanup(func() { os.Args = previous })
	if err := run(); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(".")
	if err != nil || len(files) != 0 {
		t.Fatal("version command wrote runtime files", files, err)
	}
}

func TestBackupUsesWorkingDirectoryForRuntimeFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", config.Example(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_NAME", "my-app")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
	t.Setenv("DATA_DIR", "")
	previous := os.Args
	os.Args = []string{"donate", "backup"}
	t.Cleanup(func() { os.Args = previous })
	if err := run(); err != nil {
		t.Fatal(err)
	}
	date := time.Now().Format("2006-01-02")
	for _, name := range []string{"data/my-app.db", "data/my-app.lock", "data/logs/my-app_" + date + ".log", "data/logs/my-app_" + date + ".jsonl"} {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("runtime file %s is not in the working directory: %v", name, err)
		}
	}
	backups, err := filepath.Glob(filepath.Join("data", "backups", "my-app-*.db"))
	if err != nil || len(backups) != 1 {
		t.Errorf("expected one local backup, got %v, %v", backups, err)
	}
	for _, name := range []string{"donate.db", "logs", "backups"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("unexpected nested runtime directory %s: %v", name, err)
		}
	}
}
