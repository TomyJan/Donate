package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TomyJan/Donate/internal/observability"
	"github.com/TomyJan/Donate/internal/storage"
)

func logStorageFailure(log *slog.Logger, err error, operation, path string, started time.Time) {
	phase, source, target := "unknown", "", ""
	var detail *storage.OperationError
	if errors.As(err, &detail) {
		operation, phase = detail.Operation, detail.Phase
		if detail.Path != "" {
			path = detail.Path
		}
		source, target = detail.Source, detail.Target
	}
	reason := observability.ErrorReason(err)
	log.Error(fmt.Sprintf("数据库 %s 执行 %s 在阶段 %s 失败，原因 %s，耗时 %d 毫秒", path, operation, phase, reason, time.Since(started).Milliseconds()), "operation", operation, "stage", phase, "path", path, "source", source, "target", target, "reason", reason, "duration_ms", time.Since(started).Milliseconds())
}

func cleanupExpired(ctx context.Context, db *storage.Store, path string, log *slog.Logger, now time.Time) {
	for _, item := range []struct {
		name, query string
		before      int64
	}{
		{"auth_flows", "DELETE FROM auth_flows WHERE expires_at<?", now.Unix()},
		{"sessions", "DELETE FROM sessions WHERE expires_at<?", now.Add(-24 * time.Hour).Unix()},
	} {
		started := time.Now()
		result, err := db.DB.ExecContext(ctx, item.query, item.before)
		if err != nil {
			logStorageFailure(log, &storage.OperationError{Operation: "cleanup", Phase: item.name, Path: path, Err: err}, "cleanup", path, started)
			continue
		}
		if count, err := result.RowsAffected(); err == nil && count > 0 {
			log.Debug(fmt.Sprintf("数据库 %s 已清理 %s 的 %d 条过期记录", path, item.name, count), "operation", "cleanup", "table", item.name, "path", path, "count", count, "duration_ms", time.Since(started).Milliseconds())
		}
	}
}
