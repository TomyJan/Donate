package storage

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenRefusesInterruptedRestoreWithoutCreatingDatabase(t *testing.T) {
	for _, suffixes := range [][]string{{".before-restore-"}, {".restore-"}, {".before-restore-", ".restore-"}} {
		t.Run(strings.Join(suffixes, "and"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "donate.db")
			artifacts := map[string]string{}
			for _, suffix := range suffixes {
				artifact := path + suffix + NewID().String()
				artifacts[artifact] = "recoverable original or staged data"
				if err := os.WriteFile(artifact, []byte(artifacts[artifact]), 0600); err != nil {
					t.Fatal(err)
				}
			}
			s, err := Open(context.Background(), path)
			if err == nil {
				s.Close()
				t.Fatal("startup created a database despite interrupted restore artifacts")
			}
			if !strings.Contains(err.Error(), "恢复") || !strings.Contains(err.Error(), "donate.db") {
				t.Fatalf("unhelpful error: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("startup touched missing live database: %v", err)
			}
			for artifact, want := range artifacts {
				got, err := os.ReadFile(artifact)
				if err != nil || string(got) != want {
					t.Fatalf("recovery artifact changed: %s %q %v", artifact, got, err)
				}
			}
		})
	}
}

func TestOpenFreshDatabaseWithoutRestoreArtifacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "donate.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var count int
	if err := s.DB.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatalf("fresh database: count=%d err=%v", count, err)
	}
}

func TestInterruptedRestorePathCaseMatchesPlatform(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	original := filepath.Join(dir, "DONATE.DB")
	s, err := Open(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("INSERT INTO users(id,created_at) VALUES(randomblob(16),11)"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	previous := original + ".before-restore-" + NewID().String()
	if err := os.Rename(original, previous); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(previous)
	if err != nil {
		t.Fatal(err)
	}
	lower := filepath.Join(dir, "donate.db")
	reopened, err := Open(ctx, lower)
	if runtime.GOOS == "windows" {
		if err == nil {
			reopened.Close()
			t.Fatal("Windows path alias silently created an empty database")
		}
		if _, statErr := os.Stat(lower); !os.IsNotExist(statErr) {
			t.Fatalf("missing live database was created: %v", statErr)
		}
	} else {
		if err != nil {
			t.Fatalf("unrelated case-sensitive path blocked fresh database: %v", err)
		}
		reopened.Close()
	}
	after, err := os.ReadFile(previous)
	if err != nil || string(after) != string(before) {
		t.Fatalf("original recovery database changed: %v", err)
	}
}
