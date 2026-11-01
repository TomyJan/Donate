package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestHTTPCompletionLogsActualResponseStatus(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		status             int
		handler            http.HandlerFunc
	}{
		{"router HEAD rejection", http.MethodHead, "/health/live", 405, nil},
		{"direct error", http.MethodGet, "/direct", 503, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }},
		{"flushed error", http.MethodHead, "/direct", 502, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502); w.(http.Flusher).Flush() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, h, _ := setup(t)
			var logs bytes.Buffer
			s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
			if tc.handler != nil {
				router := chi.NewRouter()
				router.Use(s.middleware)
				router.Method(tc.method, tc.path, tc.handler)
				h = router
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("HTTP status=%d, want %d", w.Code, tc.status)
			}
			found := false
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				if line == "" {
					continue
				}
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if entry["status"] == float64(tc.status) && entry["code"] != "OK" && entry["level"] == "INFO" {
					found = true
				}
			}
			if !found {
				t.Fatalf("actual HTTP failure absent from INFO logs: %s", logs.String())
			}
		})
	}
}

func TestHTTPCompletionKeepsSuccessfulHEADQuiet(t *testing.T) {
	s, _, _ := setup(t)
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	router := chi.NewRouter()
	router.Use(s.middleware)
	router.Head("/probe", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/probe", nil))
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["status"] != float64(200) || entry["code"] != "OK" || entry["level"] != "DEBUG" {
		t.Fatalf("successful HEAD changed: %v", entry)
	}
}
