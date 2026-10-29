package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/config"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"github.com/TomyJan/Donate/internal/storage/dbgen"
	"log/slog"
	"sort"
	"time"
)

var ErrSelfBan = errors.New("不能封禁自己")

type Service struct {
	Store  *storage.Store
	Orders *order.Service
	Config config.Config
	Log    *slog.Logger
	Now    func() time.Time
}

func New(db *storage.Store, o *order.Service, c config.Config, l *slog.Logger) *Service {
	return &Service{db, o, c, l, time.Now}
}
func (s *Service) check(ctx context.Context, tx *sql.Tx, actor string) error {
	if !s.Config.AdminIDs[actor] {
		return auth.ErrUnauthorized
	}
	id, e := storage.ParseID(actor)
	if e != nil {
		return auth.ErrUnauthorized
	}
	status, e := dbgen.New(tx).UserStatus(ctx, id)
	if e != nil {
		return e
	}
	if status != "active" {
		return auth.ErrBanned
	}
	return nil
}
func (s *Service) Ban(ctx context.Context, actor, target string, ban bool) error {
	if actor == target && ban {
		return ErrSelfBan
	}
	tid, e := storage.ParseID(target)
	if e != nil {
		return auth.ErrInvalid
	}
	aid, _ := storage.ParseID(actor)
	action, status := "user.unban", "active"
	if ban {
		action, status = "user.ban", "banned"
	}
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		if e := s.check(ctx, tx, actor); e != nil {
			return e
		}
		r, e := tx.ExecContext(ctx, "UPDATE users SET status=? WHERE id=?", status, tid)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return order.ErrNotFound
		}
		if ban {
			if _, e = tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", s.Now().Unix(), tid); e != nil {
				return e
			}
		}
		return storage.Audit(ctx, tx, aid, action, "user", tid, status, s.Now().Unix())
	})
	if e == nil {
		s.Log.Info(fmt.Sprintf("管理员 %s 对用户 %s 执行 %s，全部会话按状态处理", actor, target, action), "actor_id", actor, "user_id", target, "action", action)
	}
	return e
}

type Item struct {
	ID        string `json:"id"`
	Result    string `json:"result"`
	JobID     string `json:"job_id,omitempty"`
	Status    string `json:"status,omitempty"`
	AmountFen int64  `json:"amount_fen,omitempty"`
}
type Operation struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	ExpiresAt time.Time `json:"expires_at"`
	Confirmed bool      `json:"confirmed"`
	Items     []Item    `json:"items"`
}

var actions = map[string]bool{"close": true, "hide_order": true, "show_order": true, "hide_message": true, "show_message": true, "delete": true, "export": true}

func (s *Service) Preview(ctx context.Context, actor, action string, ids []string) (Operation, error) {
	out := Operation{Items: []Item{}}
	if !actions[action] || len(ids) == 0 || len(ids) > 100 {
		return out, auth.ErrInvalid
	}
	set := map[string]bool{}
	clean := []string{}
	for _, id := range ids {
		b, e := storage.ParseID(id)
		if e != nil {
			return out, auth.ErrInvalid
		}
		id = storage.IDString(b)
		if !set[id] {
			set[id] = true
			clean = append(clean, id)
		}
	}
	sort.Strings(clean)
	id := storage.NewID()
	aid, _ := storage.ParseID(actor)
	now := s.Now()
	raw, _ := json.Marshal(clean)
	e := s.Store.Write(ctx, func(tx *sql.Tx) error {
		if e := s.check(ctx, tx, actor); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, "INSERT INTO admin_operations(id,actor_id,action,target_ids,created_at,expires_at) VALUES(?,?,?,?,?,?)", id[:], aid, action, string(raw), now.Unix(), now.Add(5*time.Minute).Unix())
		return e
	})
	if e != nil {
		return out, e
	}
	return s.Operation(ctx, actor, id.String())
}
func (s *Service) Operation(ctx context.Context, actor, id string) (Operation, error) {
	out := Operation{Items: []Item{}}
	bid, e := storage.ParseID(id)
	if e != nil {
		return out, order.ErrNotFound
	}
	aid, e := storage.ParseID(actor)
	if e != nil {
		return out, auth.ErrUnauthorized
	}
	var raw string
	var expires int64
	var confirmed sql.NullInt64
	e = s.Store.DB.QueryRowContext(ctx, "SELECT action,target_ids,expires_at,confirmed_at FROM admin_operations WHERE id=? AND actor_id=?", bid, aid).Scan(&out.Action, &raw, &expires, &confirmed)
	if e != nil {
		return out, e
	}
	out.ID = id
	out.ExpiresAt = time.Unix(expires, 0).UTC()
	out.Confirmed = confirmed.Valid
	if confirmed.Valid {
		rows, e := s.Store.DB.QueryContext(ctx, "SELECT a.target_id,a.result,a.job_id,COALESCE(j.status,''),COALESCE(j.last_result,'') FROM admin_operation_items a LEFT JOIN jobs j ON j.id=a.job_id WHERE a.operation_id=? ORDER BY a.id", bid)
		if e != nil {
			return out, e
		}
		defer rows.Close()
		for rows.Next() {
			var item Item
			var tid, jid []byte
			var state, result string
			if e = rows.Scan(&tid, &item.Result, &jid, &state, &result); e != nil {
				return out, e
			}
			item.ID = storage.IDString(tid)
			item.JobID = storage.IDString(jid)
			if state != "" {
				item.Status = state
				if state == "done" || state == "failed" {
					item.Result = result
				}
			}
			out.Items = append(out.Items, item)
		}
		return out, rows.Err()
	}
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return out, auth.ErrInvalid
	}
	for _, id := range ids {
		o, e := s.Orders.Get(ctx, id)
		item := Item{ID: id, Result: "ready"}
		if errors.Is(e, order.ErrNotFound) {
			item.Result = "not_found"
		} else if e != nil {
			return out, e
		} else {
			item.Status = o.Status
			item.AmountFen = o.AmountFen
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
func (s *Service) Execute(ctx context.Context, actor, id string) (Operation, error) {
	bid, e := storage.ParseID(id)
	if e != nil {
		return Operation{}, auth.ErrInvalid
	}
	aid, _ := storage.ParseID(actor)
	now := s.Now()
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		if e := s.check(ctx, tx, actor); e != nil {
			return e
		}
		var action, raw string
		var expires int64
		var done sql.NullInt64
		if e := tx.QueryRowContext(ctx, "SELECT action,target_ids,expires_at,confirmed_at FROM admin_operations WHERE id=? AND actor_id=?", bid, aid).Scan(&action, &raw, &expires, &done); e != nil {
			return e
		}
		if done.Valid {
			return nil
		}
		if expires <= now.Unix() {
			return auth.ErrInvalid
		}
		var ids []string
		if json.Unmarshal([]byte(raw), &ids) != nil {
			return auth.ErrInvalid
		}
		for _, target := range ids {
			tid, _ := storage.ParseID(target)
			o, e := order.Scan(tx.QueryRowContext(ctx, "SELECT "+order.Columns+" FROM orders WHERE id=?", tid))
			result := "updated"
			var job any
			if errors.Is(e, order.ErrNotFound) {
				result = "not_found"
			} else if e != nil {
				return e
			} else {
				switch action {
				case "delete":
					if _, e = tx.ExecContext(ctx, "DELETE FROM orders WHERE id=?", tid); e != nil {
						return e
					}
					if _, e = tx.ExecContext(ctx, "UPDATE admin_operation_items SET result='deleted',job_id=NULL WHERE target_id=?", tid); e != nil {
						return e
					}
					result = "deleted"
				case "close":
					if o.Status != "pending" {
						result = o.Status + "_skipped"
					} else {
						var jid []byte
						e = tx.QueryRowContext(ctx, "SELECT id FROM jobs WHERE kind='close' AND order_id=? AND status IN ('ready','running')", tid).Scan(&jid)
						if errors.Is(e, sql.ErrNoRows) {
							newID := storage.NewID()
							jid = newID[:]
							_, e = tx.ExecContext(ctx, "INSERT INTO jobs(id,kind,order_id,run_at,source,actor_id,request_id,created_at,updated_at) VALUES(?,'close',?,?,'admin',?,?,?,?)", jid, tid, now.Unix(), aid, id, now.Unix(), now.Unix())
						}
						if e != nil {
							return e
						}
						job = jid
						result = "queued"
					}
				case "hide_order", "show_order", "hide_message", "show_message":
					col := "hidden_order"
					if action == "hide_message" || action == "show_message" {
						col = "hidden_message"
					}
					hidden := action == "hide_order" || action == "hide_message"
					_, e = tx.ExecContext(ctx, "UPDATE orders SET "+col+"=?,version=version+1 WHERE id=?", hidden, tid)
					if e != nil {
						return e
					}
				case "export":
					result = "exported"
				}
				if e = storage.Audit(ctx, tx, aid, "order."+action, "order", tid, result, now.Unix()); e != nil {
					return e
				}
			}
			iid := storage.NewID()
			if _, e = tx.ExecContext(ctx, "INSERT INTO admin_operation_items(id,operation_id,target_id,result,job_id) VALUES(?,?,?,?,?)", iid[:], bid, tid, result, job); e != nil {
				return e
			}
		}
		_, e := tx.ExecContext(ctx, "UPDATE admin_operations SET confirmed_at=? WHERE id=?", now.Unix(), bid)
		return e
	})
	if e != nil {
		return Operation{}, e
	}
	s.Log.Info(fmt.Sprintf("管理员 %s 确认执行管理操作 %s", actor, id), "actor_id", actor, "operation_id", id)
	return s.Operation(ctx, actor, id)
}
