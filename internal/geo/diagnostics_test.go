package geo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

type failedDatabaseReader struct{ err error }

func (r failedDatabaseReader) Read([]byte) (int, error) { return 0, r.err }

func TestStagedDatabaseWriteKeepsFilesystemAndReadCauses(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		err := copyDatabase(filepath.Join(t.TempDir(), "absent", "stage.mmdb"), strings.NewReader("db"), 32)
		// Windows maps a missing parent directory to ENOTDIR; Unix uses ENOENT.
		if !errors.Is(err, os.ErrNotExist) || !strings.Contains(Diagnostic(err), "stage=database_write") || !(strings.Contains(Diagnostic(err), "open_not_found") || strings.Contains(Diagnostic(err), "open_not_directory")) {
			t.Fatalf("lost write diagnostic: %s", Diagnostic(err))
		}
	})
	for _, tc := range []struct {
		name   string
		cause  error
		reason string
	}{
		{"disk_full", syscall.ENOSPC, "no_space"},
		{"permission", os.ErrPermission, "permission"},
		{"truncated", io.ErrUnexpectedEOF, "truncated_io"},
		{"canceled", context.Canceled, "canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := copyDatabase(filepath.Join(t.TempDir(), "stage.mmdb"), failedDatabaseReader{tc.cause}, 32)
			if !errors.Is(err, tc.cause) || !strings.Contains(Diagnostic(err), "stage=database_write") || !strings.Contains(Diagnostic(err), tc.reason) {
				t.Fatalf("lost copy diagnostic: %s", Diagnostic(err))
			}
		})
	}
}

func TestDatabaseRollbackFailureLogsRetainedRecoveryPath(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, ".ipdb-rollback-safe")
	if err := os.WriteFile(backup, []byte("healthy original"), 0600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s := &Service{log: slog.New(slog.NewJSONHandler(&logs, nil))}
	err := s.rollbackDatabase(WithTask(context.Background(), "restore-job", "", "restore-request"), databaseSource{kind: "mmdb", path: filepath.Join(root, "absent", "city.mmdb"), url: "https://example.com/?key=secret-key"}, backup)
	if err == nil || !strings.Contains(Diagnostic(err), "database_rollback") {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("missing rollback log: %s: %v", logs.String(), err)
	}
	if entry["rollback_path"] != backup || entry["job_id"] != "restore-job" || (entry["reason"] != "not_found" && entry["reason"] != "not_directory") || strings.Contains(logs.String(), "secret-key") {
		t.Fatal(entry)
	}
	if contents, err := os.ReadFile(backup); err != nil || string(contents) != "healthy original" {
		t.Fatal(string(contents), err)
	}
}

func TestLibraryFilesystemFailureRetainsStageAndCause(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-directory")
	if err := os.WriteFile(parent, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Config{GeoMode: "local", GeoPath: filepath.Join(parent, "city.mmdb"), GeoMMDBURL: "https://example.com/db?key=never-log-me", GeoUpdateDays: 7}
	err := NewUpdater(nil, c, slog.Default()).Update(context.Background(), "mmdb")
	var fileErr *os.PathError
	if !errors.As(err, &fileErr) || !strings.Contains(Diagnostic(err), "download_prepare") || strings.Contains(Diagnostic(err), "never-log-me") {
		t.Fatalf("lost filesystem diagnostic: %s (cause %T)", Diagnostic(err), err)
	}
}

func TestOnlineFailureLogsStageIPSourceAndSafeReason(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body, reason string
	}{
		{"http", 503, "private upstream body", "http_status"},
		{"json", 200, "private upstream body", "invalid_json"},
		{"ip", 200, "{\"ret\":200,\"data\":{\"ip\":\"8.8.8.8\",\"country\":\"中国\",\"prov\":\"江苏\"}}", "ip_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			s, err := New(config.Config{GeoMode: "online"}, slog.New(slog.NewJSONHandler(&logs, nil)))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			s.sources = s.sources[:1]
			s.sources[0].endpoint = "https://example.com/query?secret=do-not-log&ip="
			s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			_, err = s.Lookup(WithTask(context.Background(), "job-123", "order-456", "request-789"), "1.1.1.1")
			if !errors.Is(err, ErrUnavailable) {
				t.Fatal(err)
			}
			for _, v := range []string{"1.1.1.1", "ip9", tc.reason, "duration_ms", "job-123", "order-456", "request-789"} {
				if !strings.Contains(logs.String(), v) {
					t.Fatalf("missing %q in diagnostic %s", v, logs.String())
				}
			}
			for _, secret := range []string{"do-not-log", "private upstream body"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("unsafe diagnostic", logs.String())
				}
			}
		})
	}
}
