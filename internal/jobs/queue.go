package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"log/slog"
	"strings"
	"time"
)

type Queue struct {
	Store *storage.Store
	Log   *slog.Logger
	Now   func() time.Time
}

func NewQueue(s *storage.Store, l *slog.Logger) *Queue { return &Queue{s, l, time.Now} }

type Job struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	OrderID    string    `json:"order_id"`
	Status     string    `json:"status"`
	Attempts   int       `json:"attempts"`
	Source     string    `json:"source"`
	ActorID    string    `json:"actor_id,omitempty"`
	RequestID  string    `json:"request_id"`
	Result     string    `json:"result"`
	LeaseToken string    `json:"-"`
	RunAt      time.Time `json:"run_at"`
}

func (q *Queue) Enqueue(ctx context.Context, kind, orderID, source, actor, requestID string) (string, error) {
	return q.enqueue(ctx, kind, orderID, source, actor, requestID, "")
}
func (q *Queue) EnqueueOnce(ctx context.Context, orderID, actor, requestID, key string) (string, error) {
	if len(key) < 1 || len(key) > 128 {
		return "", order.ErrInvalid
	}
	return q.enqueue(ctx, "payment", orderID, "manual", actor, requestID, key)
}
func (q *Queue) enqueue(ctx context.Context, kind, orderID, source, actor, requestID, key string) (string, error) {
	if kind != "payment" && kind != "geo" && kind != "close" {
		return "", errors.New("无效任务类型")
	}
	oid, e := storage.ParseID(orderID)
	if e != nil {
		return "", e
	}
	var aid any
	if actor != "" {
		aid, e = storage.ParseID(actor)
		if e != nil {
			return "", e
		}
	}
	now := q.Now()
	var result []byte
	e = q.Store.Write(ctx, func(tx *sql.Tx) error {
		digest := sha256.Sum256([]byte(orderID))
		if key != "" {
			var previous []byte
			err := tx.QueryRowContext(ctx, "SELECT digest,target_id FROM idempotency_records WHERE caller_id=? AND action='order.reconcile' AND key=?", aid, key).Scan(&previous, &result)
			if err == nil {
				if string(previous) != string(digest[:]) {
					return order.ErrConflict
				}
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		record := func() error {
			if key == "" {
				return nil
			}
			id := storage.NewID()
			_, e := tx.ExecContext(ctx, "INSERT INTO idempotency_records(id,caller_id,action,key,digest,target_id,created_at,expires_at) VALUES(?,?,'order.reconcile',?,?,?,?,?)", id[:], aid, key, digest[:], result, now.Unix(), now.Add(24*time.Hour).Unix())
			return e
		}
		var status string
		e := tx.QueryRowContext(ctx, "SELECT id,status FROM jobs WHERE kind=? AND order_id=? AND status IN ('ready','running')", kind, oid).Scan(&result, &status)
		if e == nil {
			if status == "running" {
				_, e = tx.ExecContext(ctx, "UPDATE jobs SET rerun=1,source=?,actor_id=?,request_id=?,updated_at=? WHERE id=?", source, aid, requestID, now.Unix(), result)
			} else {
				_, e = tx.ExecContext(ctx, "UPDATE jobs SET run_at=?,source=?,actor_id=?,request_id=?,updated_at=? WHERE id=?", now.Unix(), source, aid, requestID, now.Unix(), result)
			}
			if e != nil {
				return e
			}
			return record()
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		id := storage.NewID()
		result = id[:]
		_, e = tx.ExecContext(ctx, "INSERT INTO jobs(id,kind,order_id,run_at,source,actor_id,request_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)", id[:], kind, oid, now.Unix(), source, aid, requestID, now.Unix(), now.Unix())
		if e != nil {
			return e
		}
		return record()
	})
	if e == nil {
		q.Log.Info(fmt.Sprintf("订单 %s 的 %s 任务 %s 已唤醒，来源 %s，请求 %s", orderID, kind, storage.IDString(result), source, requestID), "order_id", orderID, "job_id", storage.IDString(result), "request_id", requestID, "source", source)
	}
	return storage.IDString(result), e
}

const columns = "id,kind,order_id,status,attempts,source,actor_id,request_id,last_result,lease_token,run_at"

func scan(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var id, oid, aid []byte
	var lease sql.NullString
	var run int64
	e := row.Scan(&id, &j.Kind, &oid, &j.Status, &j.Attempts, &j.Source, &aid, &j.RequestID, &j.Result, &lease, &run)
	j.ID, j.OrderID, j.ActorID, j.LeaseToken = storage.IDString(id), storage.IDString(oid), storage.IDString(aid), lease.String
	j.RunAt = time.Unix(run, 0).UTC()
	return j, e
}
func (q *Queue) Claim(ctx context.Context, kinds []string) (Job, error) {
	var out Job
	now := q.Now()
	args := []any{now.Unix(), now.Unix()}
	for _, k := range kinds {
		args = append(args, k)
	}
	if len(kinds) == 0 {
		return out, sql.ErrNoRows
	}
	holders := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
	e := q.Store.Write(ctx, func(tx *sql.Tx) error {
		j, e := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM jobs WHERE ((status='ready' AND run_at<=?) OR (status='running' AND lease_until<=?)) AND kind IN ("+holders+") ORDER BY CASE kind WHEN 'close' THEN 0 ELSE 1 END,run_at,id LIMIT 1", args...))
		if e != nil {
			return e
		}
		id, _ := storage.ParseID(j.ID)
		lease := storage.NewID().String()
		_, e = tx.ExecContext(ctx, "UPDATE jobs SET status='running',attempts=attempts+1,lease_token=?,lease_until=?,updated_at=? WHERE id=?", lease, now.Add(time.Minute).Unix(), now.Unix(), id)
		j.Status = "running"
		j.LeaseToken = lease
		j.Attempts++
		out = j
		return e
	})
	if e == nil {
		q.Log.Debug(fmt.Sprintf("任务 %s 领取订单 %s，尝试 %d", out.ID, out.OrderID, out.Attempts), "job_id", out.ID, "order_id", out.OrderID, "attempt", out.Attempts)
	}
	return out, e
}
func (q *Queue) Finish(ctx context.Context, j Job, status, result string, next time.Time) (bool, error) {
	id, e := storage.ParseID(j.ID)
	if e != nil {
		return false, e
	}
	now := q.Now()
	changed := false
	e = q.Store.Write(ctx, func(tx *sql.Tx) error {
		var rerun int
		var lease string
		var until int64
		e := tx.QueryRowContext(ctx, "SELECT rerun,lease_token,lease_until FROM jobs WHERE id=? AND status='running'", id).Scan(&rerun, &lease, &until)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if lease != j.LeaseToken || until <= now.Unix() {
			return nil
		}
		if rerun == 1 {
			status = "ready"
			next = now
		}
		run := next.Unix()
		if next.IsZero() {
			run = now.Unix()
		}
		r, e := tx.ExecContext(ctx, "UPDATE jobs SET status=?,last_result=?,run_at=?,lease_token=NULL,lease_until=NULL,rerun=0,updated_at=? WHERE id=? AND lease_token=?", status, result, run, now.Unix(), id, j.LeaseToken)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		changed = n == 1
		return nil
	})
	return changed, e
}
func (q *Queue) Get(ctx context.Context, id string) (Job, error) {
	b, e := storage.ParseID(id)
	if e != nil {
		return Job{}, e
	}
	return scan(q.Store.DB.QueryRowContext(ctx, "SELECT "+columns+" FROM jobs WHERE id=?", b))
}
