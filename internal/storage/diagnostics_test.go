package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupFailureIdentifiesSnapshotPhaseAndPreservesCause(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, filepath.Join(dir, "donate.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	_, original := s.DB.ExecContext(ctx, "SELECT 1")
	_, err = s.Backup(ctx, filepath.Join(dir, "backups"))
	var operation *OperationError
	if !errors.As(err, &operation) || operation.Operation != "backup" || operation.Phase != "snapshot" || !strings.HasPrefix(operation.Path, filepath.Join(dir, "backups")) {
		t.Fatalf("structured backup context missing: %v", err)
	}
	if err == nil || !errors.Is(err, original) || !strings.Contains(err.Error(), "backup snapshot") || !strings.Contains(err.Error(), filepath.ToSlash(filepath.Join(dir, "backups"))) {
		t.Fatalf("backup phase/path/cause lost: %v", err)
	}
}

func TestRestoreMissingSourceIdentifiesPathsAndPreservesCause(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "missing.db"), filepath.Join(dir, "donate.db")
	err := Restore(context.Background(), source, target)
	var operation *OperationError
	if !errors.As(err, &operation) || operation.Operation != "restore" || operation.Phase != "source_stat" || operation.Source != source || operation.Target != target {
		t.Fatalf("structured restore context missing: %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "restore source_stat") || !strings.Contains(err.Error(), filepath.ToSlash(source)) || !strings.Contains(err.Error(), filepath.ToSlash(target)) {
		t.Fatalf("restore phase/path/cause lost: %v", err)
	}
}

func TestOpenCorruptDatabaseIdentifiesPingAndPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.db")
	if err := os.WriteFile(path, []byte("this is not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), path)
	var operation *OperationError
	if !errors.As(err, &operation) || operation.Operation != "open" || operation.Phase != "ping" || operation.Path != path {
		t.Fatalf("structured open context missing: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "open ping") || !strings.Contains(err.Error(), filepath.ToSlash(path)) {
		t.Fatalf("database open context lost: %v", err)
	}
}
