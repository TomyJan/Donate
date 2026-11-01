package main

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/storage"
)

func TestStorageFailureLogKeepsSafePhaseAndLocation(t *testing.T) {
	var logs bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&logs, nil))
	err := &storage.OperationError{Operation: "backup", Phase: "snapshot", Path: "data/donate.db", Err: errors.New("private SQL parameters token=do-not-log")}
	logStorageFailure(l, err, "backup", "data/donate.db", time.Now())
	for _, want := range []string{"backup", "snapshot", "data/donate.db", "duration_ms", "internal_error"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing %q: %s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), "do-not-log") || strings.Contains(logs.String(), "SQL parameters") {
		t.Fatal("unsafe error logged", logs.String())
	}
}
