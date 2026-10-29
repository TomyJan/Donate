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
		r.retry(ctx, j, o, "query_failed")
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
func (r *Runner) retry(ctx context.Context, j Job, o order.Order, result string) {
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
			r.Queue.Log.Error(fmt.Sprintf("订单 %s 的任务 %s 在自动窗口结束时仍无法核验，需手动补单", o.ID, j.ID), "order_id", o.ID, "job_id", j.ID, "result", result)
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
	r.Queue.Log.Warn(fmt.Sprintf("订单 %s 的任务 %s 第 %d 次执行结果 %s，将于 %s 重试", o.ID, j.ID, j.Attempts, result, next.Format(time.RFC3339)), "order_id", o.ID, "job_id", j.ID, "attempt", j.Attempts, "next_at", next)
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
			r.retry(ctx, j, o, "close_unknown")
			return
		}
		confirmed = true
	}
	oid, _ := storage.ParseID(o.ID)
	jid, _ := storage.ParseID(j.ID)
	now := r.Queue.Now()
	result := "local_closed_remote_unconfirmed"
	remote := "unsupported"
	if confirmed {
		result = "remote_closed"
		remote = "confirmed"
	}
	e := r.Queue.Store.Write(ctx, func(tx *sql.Tx) error {
		var n int
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE id=? AND lease_token=? AND status='running' AND lease_until>?", jid, j.LeaseToken, now.Unix()).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return order.ErrBusy
		}
		_, e := tx.ExecContext(ctx, "UPDATE orders SET status='closed',close_reason=CASE WHEN close_reason='' THEN 'admin' ELSE close_reason END,closed_at=COALESCE(closed_at,?),remote_close_status=?,remote_closed_at=CASE WHEN ? THEN ? ELSE NULL END,version=version+1 WHERE id=? AND status!='paid'", now.Unix(), remote, confirmed, now.Unix(), oid)
		if e != nil {
			return e
		}
		return storage.Audit(ctx, tx, nil, "order.close", "order", oid, result, now.Unix())
	})
	if e != nil {
		r.retry(ctx, j, o, "close_save_failed")
		return
	}
	// Native EPay status 0 is unpaid, not proof of remote closure. Only the signed close ACK is stored as confirmation.
	r.finish(ctx, j, "done", result, time.Time{})
	if !confirmed {
		_, _ = r.Queue.Enqueue(ctx, "payment", o.ID, "close_followup", j.ActorID, j.RequestID)
	}
}
func (r *Runner) geolocate(ctx context.Context, j Job, o order.Order) {
	if r.Geo == nil {
		r.finish(ctx, j, "done", "disabled", time.Time{})
		return
	}
	p, e := r.Geo.Lookup(ctx, o.IP)
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
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE id=? AND lease_token=? AND status='running' AND lease_until>?", jid, j.LeaseToken, r.Queue.Now().Unix()).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return order.ErrBusy
		}
		_, e := tx.ExecContext(ctx, "INSERT INTO order_locations(id,order_id,country,province,city,district,street,isp,asn,latitude,longitude,accuracy_radius,source,source_version,queried_at) SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM orders WHERE id=?) ON CONFLICT(order_id) DO NOTHING", lid[:], oid, p.Country, p.Province, p.City, p.District, p.Street, p.ISP, p.ASN, p.Latitude, p.Longitude, p.AccuracyRadius, p.Source, p.Version, p.QueriedAt.Unix(), oid)
		return e
	})
	if e != nil {
		r.finish(ctx, j, "failed", "save_failed", time.Time{})
		return
	}
	r.finish(ctx, j, "done", "located_"+p.Source, time.Time{})
}
