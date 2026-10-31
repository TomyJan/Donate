package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func (s *Service) RetryTasks(ctx context.Context, actor string, ids []string, key, requestID string) (TaskRetryResult, error) {
	out := TaskRetryResult{Items: []TaskRetryItem{}}
	if len(ids) == 0 || len(ids) > 100 || len(key) > 128 || strings.TrimSpace(key) == "" || strings.IndexFunc(key, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return out, auth.ErrInvalid
	}
	clean := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		bid, err := storage.ParseID(id)
		if err != nil {
			return out, auth.ErrInvalid
		}
		id = storage.IDString(bid)
		if !seen[id] {
			seen[id] = true
			clean = append(clean, id)
		}
	}
	sorted := append([]string{}, clean...)
	sort.Strings(sorted)
	raw, _ := json.Marshal(sorted)
	digest := sha256.Sum256(raw)
	aid, _ := storage.ParseID(actor)
	now := s.Now()
	logOrders := map[string]string{}
	replayed := false
	err := s.Store.Write(ctx, func(tx *sql.Tx) error {
		if err := s.check(ctx, tx, actor); err != nil {
			return err
		}
		var previous []byte
		var response string
		err := tx.QueryRowContext(ctx, "SELECT digest,response FROM task_retry_requests WHERE actor_id=? AND key=?", aid, key).Scan(&previous, &response)
		if err == nil {
			if !bytes.Equal(previous, digest[:]) {
				return order.ErrConflict
			}
			replayed = true
			return json.Unmarshal([]byte(response), &out)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, id := range clean {
			bid, _ := storage.ParseID(id)
			var oid []byte
			var kind, status, orderStatus, source string
			var run int64
			err = tx.QueryRowContext(ctx, "SELECT j.order_id,j.kind,j.status,j.run_at,o.status,j.source FROM jobs j JOIN orders o ON o.id=j.order_id WHERE j.id=?", bid).Scan(&oid, &kind, &status, &run, &orderStatus, &source)
			item := TaskRetryItem{ID: id}
			if errors.Is(err, sql.ErrNoRows) {
				item.Result = "not_found"
				out.Items = append(out.Items, item)
				continue
			}
			if err != nil {
				return err
			}
			logOrders[id] = storage.IDString(oid)
			switch {
			case status == "running":
				item.Result = "running"
			case status == "done":
				item.Result = "done"
			case kind != "geo" && orderStatus == "paid":
				item.Result = "not_applicable"
			case kind == "geo" && s.Config.GeoMode == "disabled":
				item.Result = "not_applicable"
			case status == "ready" && run <= now.Unix():
				item.Result = "already_ready"
			case status == "ready" || status == "failed":
				var active int
				if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE kind=? AND order_id=? AND id!=? AND status IN ('ready','running')", kind, oid, bid).Scan(&active); err != nil {
					return err
				}
				if active > 0 {
					item.Result = "active_conflict"
				} else {
					// Preserve geo's admin_refresh source: changing it would silently remove
					// the forced-refresh behavior. Ready tasks retain their retry budget.
					if status == "failed" {
						_, err = tx.ExecContext(ctx, "UPDATE jobs SET status='ready',attempts=0,run_at=?,lease_token=NULL,lease_until=NULL,rerun=0,retry_generation=retry_generation+1,actor_id=?,request_id=?,updated_at=? WHERE id=? AND status='failed'", now.Unix(), aid, requestID, now.Unix(), bid)
					} else {
						_, err = tx.ExecContext(ctx, "UPDATE jobs SET run_at=?,retry_generation=retry_generation+1,actor_id=?,request_id=?,updated_at=? WHERE id=? AND status='ready' AND run_at>?", now.Unix(), aid, requestID, now.Unix(), bid, now.Unix())
					}
					if err != nil {
						return err
					}
					item.Result = "queued"
					out.Queued++
				}
			default:
				item.Result = "not_applicable"
			}
			if err = storage.Audit(ctx, tx, aid, "task.retry", "order", oid, item.Result, now.Unix()); err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return err
		}
		id := storage.NewID()
		_, err = tx.ExecContext(ctx, "INSERT INTO task_retry_requests(id,actor_id,key,digest,response,created_at) VALUES(?,?,?,?,?,?)", id[:], aid, key, digest[:], string(raw), now.Unix())
		return err
	})
	if err == nil && !replayed {
		for _, item := range out.Items {
			s.Log.Info(fmt.Sprintf("管理员 %s 请求订单 %s 的任务 %s 重排，结果 %s，请求 %s", actor, logOrders[item.ID], item.ID, item.Result, requestID), "actor_id", actor, "order_id", logOrders[item.ID], "job_id", item.ID, "request_id", requestID, "result", item.Result)
		}
	}
	return out, err
}
