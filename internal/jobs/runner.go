package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/payment"
	"github.com/TomyJan/Donate/internal/storage"
)

type Gateway interface {
	Ready() bool
	Query(context.Context, order.Order) (order.Observation, error)
	Close(context.Context, order.Order) error
}
type Locator interface {
	Lookup(context.Context, string) (geo.Location, error)
}
type Runner struct {
	Queue         *Queue
	Orders        *order.Service
	Gateway       Gateway
	Geo           Locator
	mu            sync.Mutex
	next, blocked time.Time
	failures      int
}

var checkpoints = []time.Duration{time.Minute, 3 * time.Minute, 8 * time.Minute, 18 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour}

func NextPayment(created, now time.Time) (time.Time, bool) {
	for _, offset := range checkpoints {
		at := created.Add(offset)
		if at.After(now) {
			return at, true
		}
	}
	return time.Time{}, false
}
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { r.loop(ctx, []string{"close", "payment"}) })
	}
	wg.Go(func() { r.loop(ctx, []string{"geo"}) })
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if e := r.Orders.Expire(ctx); e != nil && ctx.Err() == nil {
			r.Queue.Log.Error("到期扫描未完成，将于下一周期重试", "error", e)
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
		}
	}
}
func (r *Runner) loop(ctx context.Context, kinds []string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		j, e := r.Queue.Claim(ctx, kinds)
		if e == nil {
			r.Execute(ctx, j)
			continue
		}
		if !errors.Is(e, sql.ErrNoRows) && ctx.Err() == nil {
			r.Queue.Log.Error("任务领取失败，将重试", "error", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// pace bounds the complete merchant query/close stream, including recovery after outage.
func (r *Runner) pace(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	at := r.next
	if r.blocked.After(at) {
		at = r.blocked
	}
	if d := time.Until(at); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	r.next = time.Now().Add(time.Second)
	return nil
}
func (r *Runner) health(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.failures = 0
	} else {
		r.failures++
		var retry interface{ RetryAfter() time.Duration }
		if errors.As(err, &retry) {
			if at := time.Now().Add(retry.RetryAfter()); at.After(r.blocked) {
				r.blocked = at
			}
		}
		if r.failures >= 3 {
			if at := time.Now().Add(time.Duration(min(r.failures-2, 10)) * 30 * time.Second); at.After(r.blocked) {
				r.blocked = at
			}
		}
	}
}
func (r *Runner) Execute(parent context.Context, j Job) {
	ctx, cancel := context.WithTimeout(parent, 50*time.Second)
	defer cancel()
	o, e := r.Orders.Get(ctx, j.OrderID)
	if e != nil {
		r.finish(ctx, j, "done", "target_missing", time.Time{})
		return
	}
	if j.Kind == "geo" {
		r.geolocate(ctx, j, o)
		return
	}
	if j.Kind == "close" {
		// Local closure must survive gateway outages and exhausted remote retries.
		if e = r.persistClose(ctx, j, o, ""); e != nil {
			r.retry(ctx, j, o, "close_save_failed")
			return
		}
	}
	if !r.Gateway.Ready() {
		r.retry(ctx, j, o, "gateway_not_configured")
		return
	}
	if e = r.pace(ctx); e != nil {
		r.retry(ctx, j, o, "rate_wait")
		return
	}
	queryCtx, queryCancel := context.WithTimeout(ctx, 8*time.Second)
	obs, e := r.Gateway.Query(queryCtx, o)
	queryCancel()
	r.health(e)
	if e != nil {
		r.retry(ctx, j, o, "query_failed", e)
		return
	}
	obs.JobID = j.ID
	obs.LeaseToken = j.LeaseToken
	obs.Source = j.Source
	obs.RequestID = j.RequestID
	result, e := r.Orders.Apply(ctx, obs)
	if e != nil {
		if errors.Is(e, order.ErrNotFound) || errors.Is(e, order.ErrBusy) {
			return
		}
		r.retry(ctx, j, o, "validation_failed")
		return
	}
	if result.State == "paid" {
		r.finish(ctx, j, "done", "paid", time.Time{})
		return
	}
	if j.Kind == "close" {
		if o.RemoteCloseStatus == "confirmed" {
			// This query also verifies payment after an earlier close ACK.
			r.finish(ctx, j, "done", "remote_closed", time.Time{})
			return
		}
		r.close(ctx, j, o, obs)
		return
	}
	if o.Status == "closed" && o.RemoteCloseStatus == "confirmed" {
		r.finish(ctx, j, "done", "remote_closed", time.Time{})
		return
	}
	next, ok := NextPayment(o.CreatedAt, r.Queue.Now())
	if !ok {
		r.finish(ctx, j, "done", "window_ended_"+obs.State, time.Time{})
		return
	}
	r.finish(ctx, j, "ready", obs.State, next)
}
func (r *Runner) finish(ctx context.Context, j Job, status, result string, next time.Time) {
	ok, e := r.Queue.Finish(ctx, j, status, result, next)
	if e != nil {
		r.Queue.Log.Error(fmt.Sprintf("订单 %s 的任务 %s 保存执行结果失败", j.OrderID, j.ID), "order_id", j.OrderID, "job_id", j.ID, "error", e)
	} else if ok {
		r.Queue.Log.Info(fmt.Sprintf("订单 %s 的任务 %s 本次结果 %s，任务状态 %s", j.OrderID, j.ID, result, status), "order_id", j.OrderID, "job_id", j.ID, "result", result, "next_at", next)
	}
}
func (r *Runner) retry(ctx context.Context, j Job, o order.Order, result string, causes ...error) {
	reason := ""
	if len(causes) > 0 {
		reason = payment.Diagnostic(causes[0])
	}
	now := r.Queue.Now()
	delays := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}
	next := now.Add(delays[min(j.Attempts-1, 3)] + time.Duration(rand.IntN(15))*time.Second)
	r.mu.Lock()
	if r.blocked.After(next) {
		next = r.blocked
	}
	r.mu.Unlock()
	if j.Kind == "payment" {
		if !now.Before(o.CreatedAt.Add(24 * time.Hour)) {
			r.Queue.Log.Error(fmt.Sprintf("订单 %s 的任务 %s 在自动窗口结束时仍无法核验，需手动补单，原因 %s", o.ID, j.ID, reason), "order_id", o.ID, "job_id", j.ID, "result", result, "error", reason)
			r.finish(ctx, j, "failed", result, time.Time{})
			return
		}
		if now.Before(o.ExpiresAt) && next.After(o.ExpiresAt) {
			next = o.ExpiresAt
		}
	}
	if j.Kind == "close" && j.Attempts >= 5 {
		r.finish(ctx, j, "failed", result, time.Time{})
		return
	}
	message := fmt.Sprintf("订单 %s 的任务 %s 第 %d 次执行结果 %s，将于 %s 重试", o.ID, j.ID, j.Attempts, result, next.Format(time.RFC3339))
	if reason != "" {
		message += "，原因 " + reason
	}
	r.Queue.Log.Warn(message, "order_id", o.ID, "job_id", j.ID, "attempt", j.Attempts, "next_at", next, "error", reason)
	r.finish(ctx, j, "ready", result, next)
}
func (r *Runner) close(ctx context.Context, j Job, o order.Order, obs order.Observation) {
	if obs.State != "unpaid" {
		r.finish(ctx, j, "done", obs.State+"_skipped", time.Time{})
		return
	}
	confirmed := false
	if o.Protocol == "v2" {
		if e := r.pace(ctx); e != nil {
			r.retry(ctx, j, o, "close_wait")
			return
		}
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		e := r.Gateway.Close(c, o)
		cancel()
		r.health(e)
		if e != nil {
			r.retry(ctx, j, o, "close_unknown", e)
			return
		}
		confirmed = true
	}
	result := "local_closed_remote_unconfirmed"
	remote := "unsupported"
	if confirmed {
		result = "remote_closed"
		remote = "confirmed"
	}
	if e := r.persistClose(ctx, j, o, remote); e != nil {
		r.retry(ctx, j, o, "close_save_failed")
		return
	}
	// A close ACK alone cannot rule out a racing payment. Verify once more
	// while the same lease still owns the order, then leave retries durable.
	if confirmed {
		if e := r.pace(ctx); e != nil {
			r.retry(ctx, j, o, "final_query_wait")
			return
		}
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		final, e := r.Gateway.Query(c, o)
		cancel()
		r.health(e)
		if e != nil {
			r.retry(ctx, j, o, "final_query_failed", e)
			return
		}
		final.JobID, final.LeaseToken = j.ID, j.LeaseToken
		final.Source, final.RequestID = j.Source, j.RequestID
		applied, e := r.Orders.Apply(ctx, final)
		if errors.Is(e, order.ErrBusy) || errors.Is(e, order.ErrNotFound) {
			return
		}
		if e != nil {
			r.retry(ctx, j, o, "final_validation_failed")
			return
		}
		if applied.State == "paid" {
			return // Apply commits the payment and completes all payment/close jobs.
		}
	}
	r.finish(ctx, j, "done", result, time.Time{})
}

func (r *Runner) persistClose(ctx context.Context, j Job, o order.Order, remote string) error {
	oid, _ := storage.ParseID(o.ID)
	jid, _ := storage.ParseID(j.ID)
	now := r.Queue.Now()
	followup := storage.NewID()
	created := false
	var actor any
	if j.ActorID != "" {
		actor, _ = storage.ParseID(j.ActorID)
	}
	e := r.Queue.Store.Write(ctx, func(tx *sql.Tx) error {
		var n int
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE id=? AND order_id=? AND kind='close' AND lease_token=? AND status='running' AND lease_until>?", jid, oid, j.LeaseToken, now.Unix()).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return order.ErrBusy
		}
		res, e := tx.ExecContext(ctx, "UPDATE orders SET status='closed',close_reason=CASE WHEN close_reason='' THEN 'admin' ELSE close_reason END,closed_at=COALESCE(closed_at,?),remote_close_status=CASE WHEN ?='' THEN remote_close_status ELSE ? END,remote_closed_at=CASE WHEN ?='confirmed' THEN COALESCE(remote_closed_at,?) ELSE remote_closed_at END,version=version+1 WHERE id=? AND status!='paid' AND (status!='closed' OR (?<>'' AND remote_close_status<>?))", now.Unix(), remote, remote, remote, now.Unix(), oid, remote, remote)
		if e != nil {
			return e
		}
		if remote == "" {
			// Commit the followup with local closure: neither a crash nor a
			// simultaneous payment/delete may leave a closed order untracked.
			inserted, e := tx.ExecContext(ctx, "INSERT INTO jobs(id,kind,order_id,run_at,source,actor_id,request_id,created_at,updated_at) SELECT ?,'payment',?,?,'close_followup',?,?,?,? WHERE EXISTS(SELECT 1 FROM orders WHERE id=? AND status!='paid') ON CONFLICT DO NOTHING", followup[:], oid, now.Unix(), actor, j.RequestID, now.Unix(), now.Unix(), oid)
			if e != nil {
				return e
			}
			count, e := inserted.RowsAffected()
			if e != nil {
				return e
			}
			created = count == 1
			if _, e = tx.ExecContext(ctx, "UPDATE jobs SET run_at=?,source='close_followup',actor_id=?,request_id=?,updated_at=? WHERE order_id=? AND kind='payment' AND status='ready' AND EXISTS(SELECT 1 FROM orders WHERE id=? AND status!='paid')", now.Unix(), actor, j.RequestID, now.Unix(), oid, oid); e != nil {
				return e
			}
		}
		changed, e := res.RowsAffected()
		if e != nil || changed == 0 {
			return e
		}
		result := "local_closed_remote_unconfirmed"
		if remote == "confirmed" {
			result = "remote_closed"
		}
		return storage.Audit(ctx, tx, nil, "order.close", "order", oid, result, now.Unix())
	})
	if e == nil && created {
		r.Queue.Log.Info(fmt.Sprintf("订单 %s 关闭入口时已原子保存补单任务 %s，关闭任务 %s", o.ID, followup.String(), j.ID), "order_id", o.ID, "job_id", followup.String(), "close_job_id", j.ID)
	}
	return e
}
func (r *Runner) geolocate(ctx context.Context, j Job, o order.Order) {
	refresh := j.Source == GeoRefreshSource
	if r.Geo == nil {
		status := "done"
		if refresh {
			status = "failed"
		}
		r.finish(ctx, j, status, "disabled", time.Time{})
		return
	}
	var p geo.Location
	var e error
	if refresh {
		if fresh, ok := r.Geo.(interface {
			Refresh(context.Context, string) (geo.Location, error)
		}); ok {
			p, e = fresh.Refresh(ctx, o.IP)
		} else {
			e = geo.ErrUnavailable
		}
		if e == nil && p.Country == "" {
			e = geo.ErrUnavailable
		}
	} else {
		p, e = r.Geo.Lookup(ctx, o.IP)
	}
	if errors.Is(e, geo.ErrDisabled) {
		r.finish(ctx, j, "failed", "disabled", time.Time{})
		return
	}
	if e != nil {
		if j.Attempts >= 3 {
			r.finish(ctx, j, "failed", "unknown", time.Time{})
		} else {
			delay := time.Minute
			if j.Attempts > 1 {
				delay = 10 * time.Minute
			}
			r.finish(ctx, j, "ready", "source_unavailable", r.Queue.Now().Add(delay))
		}
		return
	}
	oid, _ := storage.ParseID(o.ID)
	jid, _ := storage.ParseID(j.ID)
	lid := storage.NewID()
	e = r.Queue.Store.Write(ctx, func(tx *sql.Tx) error {
		var n int
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE id=? AND order_id=? AND kind='geo' AND source=? AND lease_token=? AND status='running' AND lease_until>?", jid, oid, j.Source, j.LeaseToken, r.Queue.Now().Unix()).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return order.ErrBusy
		}
		conflict := "DO NOTHING"
		if refresh {
			conflict = "DO UPDATE SET country=excluded.country,province=excluded.province,city=excluded.city,district=excluded.district,street=excluded.street,isp=excluded.isp,asn=excluded.asn,latitude=excluded.latitude,longitude=excluded.longitude,accuracy_radius=excluded.accuracy_radius,source=excluded.source,source_version=excluded.source_version,queried_at=excluded.queried_at"
		}
		_, e := tx.ExecContext(ctx, "INSERT INTO order_locations(id,order_id,country,province,city,district,street,isp,asn,latitude,longitude,accuracy_radius,source,source_version,queried_at) SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM orders WHERE id=?) ON CONFLICT(order_id) "+conflict, lid[:], oid, p.Country, p.Province, p.City, p.District, p.Street, p.ISP, p.ASN, p.Latitude, p.Longitude, p.AccuracyRadius, p.Source, p.Version, p.QueriedAt.Unix(), oid)
		return e
	})
	if e != nil {
		r.finish(ctx, j, "failed", "save_failed", time.Time{})
		return
	}
	r.finish(ctx, j, "done", "located_"+p.Source, time.Time{})
}
