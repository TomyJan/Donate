package admin

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Service) TaskOverviewRange(ctx context.Context, name, from, to string) (TaskOverview, error) {
	out := TaskOverview{Trend: []TaskTrend{}}
	if name == "" {
		name = "7d"
	}
	now := s.Now()
	start, end, err := overviewWindow(name, from, to, now)
	if err != nil {
		return out, err
	}
	tx, err := s.Store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var recording int64
	if err = tx.QueryRowContext(ctx, "SELECT recording_since FROM task_recording WHERE singleton=1").Scan(&recording); err != nil {
		return out, err
	}
	out.RecordingSince = time.Unix(recording, 0).UTC()
	if err = tx.QueryRowContext(ctx, "SELECT count(*),COALESCE(sum(status='ready'),0),COALESCE(sum(status='running'),0),COALESCE(sum(status='done'),0),COALESCE(sum(status='failed'),0),COALESCE(sum(status='ready' AND run_at<=?),0),COALESCE(sum(status='ready' AND run_at>?),0),COALESCE(sum(status='running' AND (lease_until IS NULL OR lease_until<=?)),0) FROM jobs", now.Unix(), now.Unix(), now.Unix()).Scan(&out.Total, &out.Counts.Ready, &out.Counts.Running, &out.Counts.Done, &out.Counts.Failed, &out.Due, &out.Scheduled, &out.StaleRunning); err != nil {
		return out, err
	}
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return out, err
	}
	if start == nil {
		var first sql.NullInt64
		if err = tx.QueryRowContext(ctx, "SELECT min(created_at) FROM task_events").Scan(&first); err != nil {
			return out, err
		}
		at := now.In(zone)
		finish := time.Date(at.Year(), at.Month(), at.Day()+1, 0, 0, 0, 0, zone)
		begin := out.RecordingSince
		if first.Valid {
			begin = time.Unix(first.Int64, 0)
		}
		if !begin.Before(finish) {
			begin = finish.AddDate(0, 0, -1)
		}
		start, end = &begin, &finish
	}
	out.From = start.UTC()
	out.To = end.UTC()
	local := start.In(zone)
	floor := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	// Count calendar dates rather than 24-hour durations: historical Shanghai
	// DST transitions must never duplicate or omit a date.
	last := end.In(zone)
	lastFloor := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, zone)
	calendarStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	calendarEnd := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, time.UTC)
	days := (calendarEnd.Unix() - calendarStart.Unix()) / 86400
	if end.After(lastFloor) {
		days++
	}
	days = max(int64(1), days)
	stepDays := (days + 89) / 90
	points := (days + stepDays - 1) / stepDays
	values := []string{}
	args := []any{}
	for i := int64(0); i < points; i++ {
		begin, finish := floor.AddDate(0, 0, int(i*stepDays)), floor.AddDate(0, 0, int((i+1)*stepDays))
		out.Trend = append(out.Trend, TaskTrend{Date: begin.Format("2006-01-02")})
		values = append(values, "(?,?,?)")
		args = append(args, i, begin.Unix(), finish.Unix())
	}
	args = append(args, taskSecond(*start), taskSecond(*end))
	rows, err := tx.QueryContext(ctx, "WITH buckets(bucket,start_at,end_at) AS (VALUES "+strings.Join(values, ",")+") SELECT b.bucket,sum(e.event='created'),sum(e.event IN ('started','recovered')),sum(e.event='completed'),sum(e.event='failed'),sum(e.event='retried') FROM buckets b JOIN task_events e ON e.created_at>=b.start_at AND e.created_at<b.end_at WHERE e.created_at>=? AND e.created_at<? AND e.event!='snapshot' GROUP BY b.bucket ORDER BY b.bucket", args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var bucket int
		var v TaskTrend
		if err = rows.Scan(&bucket, &v.Created, &v.Started, &v.Completed, &v.Failed, &v.Retried); err != nil {
			rows.Close()
			return out, err
		}
		if bucket >= 0 && bucket < len(out.Trend) {
			v.Date = out.Trend[bucket].Date
			out.Trend[bucket] = v
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
