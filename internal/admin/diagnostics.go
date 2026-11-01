package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/observability"
	"github.com/TomyJan/Donate/internal/order"
)

// Persisted SQL errors can contain target data or credentials. Log only a
// controlled cause and known IDs, leaving the original error for the caller.
func (s *Service) logMutationFailure(stage, action, actor string, targets map[string]string, started time.Time, err error) {
	reason, level := observability.ErrorReason(err), slog.LevelError
	for _, item := range []struct {
		err    error
		reason string
	}{
		{auth.ErrUnauthorized, "unauthorized"}, {auth.ErrBanned, "banned"}, {auth.ErrInvalid, "invalid"},
		{order.ErrNotFound, "not_found"}, {order.ErrConflict, "idempotency_conflict"},
		{order.ErrBusy, "busy"}, {ErrSelfBan, "self_ban"}, {ErrLastIdentity, "last_identity"},
	} {
		if errors.Is(err, item.err) {
			reason, level = item.reason, slog.LevelWarn
			break
		}
	}
	message := fmt.Sprintf("管理员 %s 的 %s 操作失败，阶段 %s，原因 %s", actor, action, stage, reason)
	attrs := []any{"actor_id", actor, "action", action, "stage", stage, "reason", reason, "result", "failed", "duration_ms", time.Since(started).Milliseconds()}
	keys := make([]string, 0, len(targets))
	for key := range targets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if targets[key] != "" {
			message += fmt.Sprintf("，%s %s", key, targets[key])
			attrs = append(attrs, key, targets[key])
		}
	}
	s.Log.Log(context.Background(), level, message, attrs...)
}
