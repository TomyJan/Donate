package geo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TomyJan/Donate/internal/observability"
)

type taskTrace struct{ jobID, orderID, requestID string }
type taskTraceKey struct{}

func WithTask(ctx context.Context, jobID, orderID, requestID string) context.Context {
	return context.WithValue(ctx, taskTraceKey{}, taskTrace{jobID, orderID, requestID})
}

func traceFields(ctx context.Context) (string, []any) {
	v, _ := ctx.Value(taskTraceKey{}).(taskTrace)
	var parts []string
	if v.orderID != "" {
		parts = append(parts, "订单 "+v.orderID)
	}
	if v.jobID != "" {
		parts = append(parts, "任务 "+v.jobID)
	}
	if v.requestID != "" {
		parts = append(parts, "请求 "+v.requestID)
	}
	prefix := ""
	if len(parts) > 0 {
		prefix = strings.Join(parts, "，") + "："
	}
	return prefix, []any{"job_id", v.jobID, "order_id", v.orderID, "request_id", v.requestID}
}

type sourceFailure struct {
	stage, reason string
	status        int
	base          error
}

func (e *sourceFailure) Error() string {
	return fmt.Sprintf("stage=%s reason=%s http_status=%d", e.stage, e.reason, e.status)
}
func (e *sourceFailure) Unwrap() error { return e.base }
func failure(stage, reason string, status int, base error) error {
	if base == nil {
		base = ErrUnavailable
	} else {
		base = errors.Join(ErrUnavailable, base)
	}
	return &sourceFailure{stage: stage, reason: reason, status: status, base: base}
}

// Diagnostic never returns upstream bodies, URLs or uncontrolled error text.
func Diagnostic(err error) string {
	var f *sourceFailure
	if errors.As(err, &f) {
		return f.Error()
	}
	switch {
	case errors.Is(err, ErrConflict):
		return "geo_source_conflict"
	case errors.Is(err, ErrIncomplete):
		return "geo_province_missing"
	case errors.Is(err, ErrDisabled):
		return "geo_disabled"
	case errors.Is(err, ErrUnavailable):
		return "geo_source_unavailable"
	default:
		return observability.ErrorReason(err)
	}
}
