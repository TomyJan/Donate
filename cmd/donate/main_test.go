package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupUsesWorkingDirectoryForRuntimeFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:8080")
	t.Setenv("DATA_DIR", "")
	previous := os.Args
	os.Args = []string{"donate", "backup"}
	t.Cleanup(func() { os.Args = previous })
	if err := run(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"donate.db", "donate.log", "donate.lock"} {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("runtime file %s is not in the working directory: %v", name, err)
		}
	}
	backups, err := filepath.Glob(filepath.Join("backups", "*.db"))
	if err != nil || len(backups) != 1 {
		t.Errorf("expected one local backup, got %v, %v", backups, err)
	}
	for _, name := range []string{"data", "logs"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("unexpected nested runtime directory %s: %v", name, err)
		}
	}
}
