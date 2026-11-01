package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/storage"
)

type LibraryUpdater interface {
	Due(time.Time) []string
	Update(context.Context, string) error
}

// ScheduleIPDB only queues work. Neither startup nor the order workers download libraries.
func (r *Runner) ScheduleIPDB(ctx context.Context) error {
	if r.IPDB == nil {
		return nil
	}
	for _, resource := range r.IPDB.Due(r.Queue.Now()) {
		if err := r.Queue.enqueueLibrary(ctx, resource); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queue) enqueueLibrary(ctx context.Context, resource string) error {
	if resource != "xdb4" && resource != "xdb6" && resource != "mmdb" {
		return errors.New("无效属地库类型")
	}
	now := q.Now()
	id := storage.NewID()
	created := false
	err := q.Store.Write(ctx, func(tx *sql.Tx) error {
		// Do not wake active backoff on every scheduler tick. Exhausted tasks
		// remain inspectable; a fresh repair cycle may start after one hour.
		res, err := tx.ExecContext(ctx, "INSERT INTO jobs(id,kind,resource,run_at,source,created_at,updated_at) SELECT ?,'geoip_update',?,?,'automatic',?,? WHERE NOT EXISTS(SELECT 1 FROM jobs WHERE kind='geoip_update' AND resource=? AND (status IN ('ready','running') OR (status='failed' AND updated_at>?))) ON CONFLICT DO NOTHING", id[:], resource, now.Unix(), now.Unix(), now.Unix(), resource, now.Add(-time.Hour).Unix())
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		created = n == 1
		return err
	})
	if err == nil && created {
		q.Log.Info(fmt.Sprintf("属地库 %s 的下载更新任务 %s 已入队", resource, id.String()), "job_id", id.String(), "resource", resource)
	}
	return err
}

func (r *Runner) updateLibrary(parent context.Context, j Job) {
	started := time.Now()
	parent = geo.WithTask(parent, j.ID, "", j.RequestID)
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	var err error
	if r.IPDB == nil {
		err = geo.ErrDisabled
	} else {
		err = r.IPDB.Update(ctx, j.Resource)
	}
	status, result, next := "done", "database_updated", time.Time{}
	refund := errors.Is(parent.Err(), context.Canceled)
	if errors.Is(err, geo.ErrDisabled) {
		result = "disabled"
	} else if err != nil {
		result = "database_update_failed"
		status = "failed"
		if j.Attempts < 5 {
			delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
			status, next = "ready", r.Queue.Now().Add(delays[max(0, min(j.Attempts-1, 3))])
		}
	}
	if refund {
		status, result, next = "ready", "database_update_interrupted", r.Queue.Now()
	}
	if err != nil {
		j.Diagnostic = geo.Diagnostic(err)
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(parent), 3*time.Second)
	defer finishCancel()
	if ok, finishErr := r.Queue.finish(finishCtx, j, status, result, next, refund); finishErr != nil {
		r.Queue.Log.Error(fmt.Sprintf("属地库 %s 的任务 %s 保存结果失败，保留租约等待恢复", j.Resource, j.ID), "resource", j.Resource, "job_id", j.ID, "error", taskDiagnostic(finishErr))
	} else if ok {
		message := fmt.Sprintf("属地库 %s 的任务 %s 第 %d 次执行结果 %s，状态 %s，耗时 %d 毫秒", j.Resource, j.ID, j.Attempts, result, status, time.Since(started).Milliseconds())
		if status == "done" {
			r.Queue.Log.Info(message, "resource", j.Resource, "job_id", j.ID, "result", result, "duration_ms", time.Since(started).Milliseconds())
		} else {
			message += "，原因 " + j.Diagnostic
			if !next.IsZero() {
				message += "，下次执行 " + next.Format(time.RFC3339)
			}
			level := slog.LevelWarn
			if status == "failed" {
				level = slog.LevelError
			}
			r.Queue.Log.Log(parent, level, message, "resource", j.Resource, "job_id", j.ID, "result", result, "next_at", next, "diagnostic", j.Diagnostic, "duration_ms", time.Since(started).Milliseconds())
		}
	}
}
