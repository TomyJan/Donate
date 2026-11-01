package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestOIDCDiscoveryBodyTimeoutDiagnostic(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	s := testAuth(t)
	s.Config.OIDCissuer, s.Config.OIDCid = upstream.URL, "moeauth"
	s.HTTP.Timeout = 100 * time.Millisecond
	var logs bytes.Buffer
	s.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	_, err := s.BeginFlow(context.Background(), "moeauth", "login", nil, "/")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("public error changed: %v", err)
	}
	found := false
	for _, entry := range diagnosticEntries(t, &logs) {
		if entry["stage"] == "oidc_discovery" && entry["reason"] == "timeout" && entry["upstream_status"] == float64(200) {
			found = true
		}
	}
	if !found {
		t.Fatalf("body timeout misclassified: %s", logs.String())
	}
	if strings.Contains(logs.String(), "context deadline") {
		t.Fatal("raw library error logged")
	}
}

func TestUpstreamBodyReadDiagnosticsPreserveReadSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		err          error
		cancel       bool
	}{
		{"normal EOF", "invalid_discovery", io.EOF, false},
		{"truncated body", "truncated_io", io.ErrUnexpectedEOF, false},
		{"deadline", "timeout", context.DeadlineExceeded, false},
		{"canceled read", "canceled", context.Canceled, false},
		{"request canceled", "canceled", errors.New("private-read-error"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: qqTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(tc.err)))}, nil
			})}
			trace := tracedClient(client)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://issuer.test/private-path?secret=private-token", nil)
			response, err := trace.client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if tc.cancel {
				cancel()
			}
			data, err := io.ReadAll(response.Body)
			if string(data) != "partial" || tc.err == io.EOF && err != nil || tc.err != io.EOF && !errors.Is(err, tc.err) {
				t.Fatalf("body read changed: %q %v", data, err)
			}
			diagnostic := trace.failure(ErrUpstream, "oidc_discovery", "invalid_discovery", errors.New("private-library-error"))
			if !errors.Is(diagnostic, ErrUpstream) {
				t.Fatal("public sentinel lost")
			}
			fields := DiagnosticFields(diagnostic)
			got := map[string]any{}
			for i := 0; i+1 < len(fields); i += 2 {
				got[fields[i].(string)] = fields[i+1]
			}
			if got["reason"] != tc.reason || got["upstream_status"] != 200 || got["upstream_host"] != "issuer.test" {
				t.Fatalf("diagnostic=%v, want %s", got, tc.reason)
			}
			raw, _ := json.Marshal(fields)
			if strings.Contains(string(raw)+diagnostic.Error(), "private-") {
				t.Fatal("read diagnostic exposes raw error or URL")
			}
		})
	}
}

func TestUpstreamBodyFromPreviousRequestCannotPolluteCurrentStage(t *testing.T) {
	for _, nextRequest := range []bool{false, true} {
		trace := tracedClient(&http.Client{Transport: qqTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(iotest.ErrReader(context.DeadlineExceeded))}, nil
		})})
		old, err := trace.client.Get("https://old.test")
		if err != nil {
			t.Fatal(err)
		}
		defer old.Body.Close()
		trace.reset()
		wantHost := ""
		if nextRequest {
			current, err := trace.client.Get("https://current.test")
			if err != nil {
				t.Fatal(err)
			}
			defer current.Body.Close()
			wantHost = "current.test"
		}
		if _, err = io.ReadAll(old.Body); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		err = trace.failure(ErrUpstream, "oidc_token", "invalid_token_response", nil)
		var diagnostic *diagnosticError
		if !errors.As(err, &diagnostic) || diagnostic.reason != "invalid_token_response" || diagnostic.host != wantHost {
			t.Fatalf("old response contaminated current stage: %+v", diagnostic)
		}
	}
}
