package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TomyJan/Donate/internal/observability"
	"github.com/TomyJan/Donate/internal/storage"
)

type diagnosticContextKey struct{}
type diagnosticContext struct{ requestID, flowID string }

// WithRequestID carries the server-generated correlation ID across auth stages.
// It never carries request URLs, codes, state, tokens or cookies.
func WithRequestID(ctx context.Context, id string) context.Context {
	d, _ := ctx.Value(diagnosticContextKey{}).(diagnosticContext)
	d.requestID = id
	return context.WithValue(ctx, diagnosticContextKey{}, d)
}

func withFlowDiagnostic(ctx context.Context, cookie string) context.Context {
	d, _ := ctx.Value(diagnosticContextKey{}).(diagnosticContext)
	first, _, _ := strings.Cut(cookie, ".")
	if id, err := storage.ParseID(first); err == nil {
		d.flowID = storage.IDString(id)
	}
	return context.WithValue(ctx, diagnosticContextKey{}, d)
}

type diagnosticError struct {
	public              error
	stage, reason, host string
	status              int
	logged              bool
}

func (e *diagnosticError) Error() string {
	for _, known := range []error{ErrUnauthorized, ErrInvalid, ErrBanned, ErrConflict, ErrUpstream} {
		if errors.Is(e.public, known) {
			return known.Error()
		}
	}
	return "认证操作未完成"
}
func (e *diagnosticError) Unwrap() error { return e.public }

func authFailure(public error, stage, reason string) error {
	return &diagnosticError{public: public, stage: stage, reason: reason}
}

func stageFailure(err error, stage string) error {
	var diagnostic *diagnosticError
	if err == nil || errors.As(err, &diagnostic) {
		return err
	}
	reason := observability.ErrorReason(err)
	switch {
	case errors.Is(err, ErrUnauthorized):
		reason = "unauthorized"
	case errors.Is(err, ErrInvalid):
		reason = "invalid_input"
	case errors.Is(err, ErrBanned):
		reason = "account_banned"
	case errors.Is(err, ErrConflict):
		reason = "identity_conflict"
	case errors.Is(err, ErrUpstream):
		reason = "upstream_unavailable"
	}
	return authFailure(err, stage, reason)
}

// DiagnosticFields contains only controlled stage/reason values and transport
// metadata. Error() and arbitrary provider response bodies are never included.
func DiagnosticFields(err error) []any {
	var diagnostic *diagnosticError
	if !errors.As(err, &diagnostic) {
		return nil
	}
	return []any{"stage", diagnostic.stage, "reason", diagnostic.reason, "upstream_host", diagnostic.host, "upstream_status", diagnostic.status}
}

// DiagnosticLogged lets the HTTP boundary avoid repeating an auth outcome.
func DiagnosticLogged(err error) bool {
	var diagnostic *diagnosticError
	return errors.As(err, &diagnostic) && diagnostic.logged
}

func (s *Service) diagnosticProvider(provider string) string {
	if provider == "qq" {
		return "qq"
	}
	if provider == "oidc" || provider == s.Config.OIDCProviderID() {
		return s.Config.OIDCProviderID()
	}
	return "unknown"
}

func (s *Service) logAuth(ctx context.Context, level slog.Level, provider, stage, result string, started time.Time, fields ...any) {
	d, _ := ctx.Value(diagnosticContextKey{}).(diagnosticContext)
	provider = s.diagnosticProvider(provider)
	message := fmt.Sprintf("认证请求 %s，流程 %s，提供方 %s，阶段 %s，结果 %s", d.requestID, d.flowID, provider, stage, result)
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "user_id" || fields[i] == "identity_id" || fields[i] == "session_id" {
			message += fmt.Sprintf("，%s %v", fields[i], fields[i+1])
		}
	}
	attrs := []any{"request_id", d.requestID, "flow_id", d.flowID, "provider_id", provider, "stage", stage, "result", result, "duration_ms", time.Since(started).Milliseconds()}
	s.Log.Log(ctx, level, message, append(attrs, fields...)...)
}

func (s *Service) logAuthFailure(ctx context.Context, provider string, err error, started time.Time) {
	var d *diagnosticError
	if !errors.As(err, &d) {
		return
	}
	s.logAuth(ctx, slog.LevelWarn, provider, d.stage, "failed", started, "reason", d.reason, "upstream_host", d.host, "upstream_status", d.status)
	d.logged = true
}

// The client is copied per operation. Capturing transport and body-read errors
// avoids parsing library error strings, which may contain complete URLs/bodies.
type upstreamTrace struct {
	mu         sync.Mutex
	client     *http.Client
	host       string
	status     int
	err        error
	generation uint64
}
type tracingTransport struct {
	base  http.RoundTripper
	trace *upstreamTrace
}

func tracedClient(client *http.Client) *upstreamTrace {
	copy := *client
	base := copy.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	t := &upstreamTrace{client: &copy}
	copy.Transport = tracingTransport{base, t}
	return t
}
func (t tracingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.trace.mu.Lock()
	t.trace.generation++
	generation := t.trace.generation
	t.trace.host, t.trace.err, t.trace.status = r.URL.Hostname(), nil, 0
	t.trace.mu.Unlock()
	response, err := t.base.RoundTrip(r)
	t.trace.mu.Lock()
	if t.trace.generation == generation {
		t.trace.err = err
		if response != nil {
			t.trace.status = response.StatusCode
		}
	}
	t.trace.mu.Unlock()
	if response != nil && response.Body != nil {
		response.Body = &tracedResponseBody{ReadCloser: response.Body, trace: t.trace, ctx: r.Context(), generation: generation}
	}
	return response, err
}

type tracedResponseBody struct {
	io.ReadCloser
	trace      *upstreamTrace
	ctx        context.Context
	generation uint64
}

func (b *tracedResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		cause := err
		if contextErr := b.ctx.Err(); contextErr != nil {
			cause = contextErr
		}
		b.trace.mu.Lock()
		if b.trace.generation == b.generation {
			b.trace.err = cause
		}
		b.trace.mu.Unlock()
	}
	return n, err
}

func (t *upstreamTrace) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.generation++
	t.host, t.err, t.status = "", nil, 0
}
func (t *upstreamTrace) failure(public error, stage, fallback string, cause error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	reason := fallback
	if t.status < 200 || t.status >= 300 {
		if t.status != 0 {
			reason = "http_status"
		}
	}
	if t.err != nil {
		cause = t.err
	}
	if cause != nil {
		if classified := observability.ErrorReason(cause); classified != "internal_error" {
			reason = classified
		}
	}
	return &diagnosticError{public: public, stage: stage, reason: reason, host: t.host, status: t.status}
}
