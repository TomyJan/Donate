package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupRetentionUsesDaysAndAppName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	backups := filepath.Join(dir, "archives")
	s, err := OpenWithOptions(ctx, filepath.Join(dir, "my-app.db"), Options{AppName: "my-app", BackupDir: backups, BackupRetentionDays: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.MkdirAll(backups, 0700); err != nil {
		t.Fatal(err)
	}
	old := "my-app-20000101T000000Z-" + NewID().String() + ".db"
	recent := "my-app-" + time.Now().UTC().Format("20060102T150405Z") + "-" + NewID().String() + ".db"
	preserved := []string{recent, "other-20000101T000000Z-old.db", "my-app-not-a-snapshot.db", "my-app-20000101T000000Z-manual-export.db", "my-app-20000101T000000Z-.db", "my-app-20000101T000000Z-not-a-uuid.db"}
	for _, name := range append([]string{old}, preserved...) {
		if err := os.WriteFile(filepath.Join(backups, name), []byte("do not load"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path, err := s.Backup(ctx, backups)
	if err != nil || !strings.HasPrefix(filepath.Base(path), "my-app-") {
		t.Fatal(path, err)
	}
	if _, err := os.Stat(filepath.Join(backups, old)); !os.IsNotExist(err) {
		t.Fatal("expired own snapshot retained", err)
	}
	for _, name := range preserved {
		if _, err := os.Stat(filepath.Join(backups, name)); err != nil {
			t.Fatal("unrelated or recent file removed", name, err)
		}
	}
}

func TestStorageOptionsRejectInvalidBackupNamesAndRetention(t *testing.T) {
	for _, options := range []Options{
		{AppName: "../escape"}, {AppName: "UPPER"}, {AppName: "ok", BackupRetentionDays: -1},
	} {
		path := filepath.Join(t.TempDir(), "db.db")
		if s, err := OpenWithOptions(context.Background(), path, options); err == nil {
			s.Close()
			t.Fatal("invalid storage options accepted", options)
		}
		if err := RestoreWithOptions(context.Background(), "missing.db", path, options); err == nil || !strings.Contains(err.Error(), "备份名称或保留天数无效") {
			t.Fatal("restore did not validate options first", options, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid options created database", err)
		}
	}
}

func TestZeroBackupRetentionPreservesExpiredFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenWithOptions(ctx, filepath.Join(dir, "donate.db"), Options{AppName: "donate", BackupRetentionDays: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := filepath.Join(dir, "donate-20000101T000000Z-"+NewID().String()+".db")
	if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("unlimited retention removed snapshot", err)
	}
}
