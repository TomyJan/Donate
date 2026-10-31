package admin

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

type TaskItem struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	OrderID         string     `json:"order_id"`
	MerchantOrderNo string     `json:"merchant_order_no"`
	Nickname        string     `json:"nickname"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	TotalAttempts   int        `json:"total_attempts"`
	Source          string     `json:"source"`
	Result          string     `json:"result"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	RunAt           time.Time  `json:"run_at"`
	LeaseUntil      *time.Time `json:"lease_until,omitempty"`
	Retryable       bool       `json:"retryable"`
}
type TaskDetail struct {
	TaskItem
	ActorID        string    `json:"actor_id,omitempty"`
	ActorName      string    `json:"actor_name,omitempty"`
	RequestID      string    `json:"request_id"`
	RecordingSince time.Time `json:"recording_since"`
}
type TaskEvent struct {
	ID         string     `json:"id"`
	Event      string     `json:"event"`
	Status     string     `json:"status"`
	Attempt    int        `json:"attempt"`
	Source     string     `json:"source"`
	Result     string     `json:"result"`
	Message    string     `json:"message"`
	CreatedAt  time.Time  `json:"created_at"`
	RunAt      *time.Time `json:"run_at,omitempty"`
	DurationMS *int64     `json:"duration_ms,omitempty"`
	ActorID    string     `json:"actor_id,omitempty"`
	ActorName  string     `json:"actor_name,omitempty"`
	RequestID  string     `json:"request_id,omitempty"`
}
type TaskCounts struct {
	Ready   int `json:"ready"`
	Running int `json:"running"`
	Done    int `json:"done"`
	Failed  int `json:"failed"`
}
type TaskTrend struct {
	Date      string `json:"date"`
	Created   int    `json:"created"`
	Started   int    `json:"started"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	Retried   int    `json:"retried"`
}
type TaskOverview struct {
	Total          int         `json:"total"`
	Counts         TaskCounts  `json:"counts"`
	Due            int         `json:"due"`
	Scheduled      int         `json:"scheduled"`
	StaleRunning   int         `json:"stale_running"`
	RecordingSince time.Time   `json:"recording_since"`
	From           time.Time   `json:"from"`
	To             time.Time   `json:"to"`
	Trend          []TaskTrend `json:"trend"`
}
type TaskRetryItem struct {
	ID     string `json:"id"`
	Result string `json:"result"`
}
type TaskRetryResult struct {
	Queued int             `json:"queued"`
	Items  []TaskRetryItem `json:"items"`
}
type TaskFilter struct {
	Search, Status, Kind, Source, From, To, Sort, Direction string
	Page, Size                                              int
}

// The queue only stores result codes and controlled diagnostics. Defense in
// depth also redacts unsafe legacy/free-form values at the management boundary.
func safeTaskText(v string) string {
	lower := strings.ToLower(v)
	for _, marker := range []string{"token", "secret", "password", "credential", "authorization", "bearer ", "sign=", "signatures", "://", "<html", "{\""} {
		if strings.Contains(lower, marker) {
			return "[redacted]"
		}
	}
	r := []rune(v)
	if len(r) > 512 {
		r = r[:512]
	}
	return strings.Map(func(c rune) rune {
		if c < 32 && c != '\n' && c != '\t' {
			return -1
		}
		return c
	}, string(r))
}

var taskSorts = map[string]string{"id": "j.id", "created_at": "j.created_at", "updated_at": "j.updated_at", "kind": "j.kind", "order_id": "j.order_id", "merchant_order_no": "o.merchant_order_no", "nickname": "nickname", "status": "j.status", "attempts": "j.attempts", "total_attempts": "j.total_attempts", "run_at": "j.run_at", "source": "j.source"}
var taskSources = map[string]bool{"automatic": true, "manual": true, "browser_return": true, "admin": true, "admin_refresh": true, "admin_retry": true, "close_followup": true}

func taskWhere(f TaskFilter) (string, []any, error) {
	if len(f.Search) > 128 || f.Page < 0 || f.Size < 0 || f.Size > 100 || f.Page > 1000000 {
		return "", nil, auth.ErrInvalid
	}
	if f.Direction != "" && f.Direction != "asc" && f.Direction != "desc" {
		return "", nil, auth.ErrInvalid
	}
	if f.Sort != "" && taskSorts[f.Sort] == "" {
		return "", nil, auth.ErrInvalid
	}
	where := "1=1"
	args := []any{}
	for _, p := range []struct {
		v, col string
		ok     bool
	}{{f.Status, "j.status", f.Status == "ready" || f.Status == "running" || f.Status == "done" || f.Status == "failed"}, {f.Kind, "j.kind", f.Kind == "payment" || f.Kind == "geo" || f.Kind == "close"}, {f.Source, "j.source", taskSources[f.Source]}} {
		if p.v != "" {
			if !p.ok {
				return "", nil, auth.ErrInvalid
			}
			where += " AND " + p.col + "=?"
			args = append(args, p.v)
		}
	}
	var from, to time.Time
	for _, p := range []struct {
		v, op string
		at    *time.Time
	}{{f.From, ">=", &from}, {f.To, "<", &to}} {
		if p.v != "" {
			v, err := time.Parse(time.RFC3339, p.v)
			if err != nil {
				return "", nil, auth.ErrInvalid
			}
			*p.at = v
			where += " AND j.created_at" + p.op + "?"
			args = append(args, taskSecond(v))
		}
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return "", nil, auth.ErrInvalid
	}
	if f.Search != "" {
		q := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.ToLower(f.Search))
		hexq := strings.ReplaceAll(q, "-", "")
		where += " AND (lower(hex(j.id)) LIKE ? ESCAPE '\\' OR lower(hex(j.order_id)) LIKE ? ESCAPE '\\' OR lower(o.merchant_order_no) LIKE ? ESCAPE '\\' OR lower(COALESCE(NULLIF(trim(o.donor_nickname),''),i.nickname,'')) LIKE ? ESCAPE '\\')"
		args = append(args, "%"+hexq+"%", "%"+hexq+"%", "%"+q+"%", "%"+q+"%")
	}
	return where, args, nil
}
func taskSecond(at time.Time) int64 {
	v := at.Unix()
	if at.Nanosecond() != 0 {
		v++
	}
	return v
}

const taskJoins = " FROM jobs j JOIN orders o ON o.id=j.order_id LEFT JOIN users u ON u.id=o.user_id LEFT JOIN identities i ON i.id=u.public_identity_id LEFT JOIN users actor ON actor.id=j.actor_id LEFT JOIN identities ai ON ai.id=actor.public_identity_id "
const taskColumns = "j.id,j.kind,j.order_id,o.merchant_order_no,COALESCE(NULLIF(trim(o.donor_nickname),''),i.nickname,'捐赠者') AS nickname,j.status,j.attempts,j.total_attempts,j.source,j.last_result,j.created_at,j.updated_at,j.run_at,j.lease_until,j.actor_id,COALESCE(ai.nickname,''),j.request_id,j.recording_since,o.status"

func scanTask(row interface{ Scan(...any) error }) (TaskDetail, error) {
	var out TaskDetail
	var id, oid, aid []byte
	var created, updated, run, recording int64
	var lease sql.NullInt64
	var orderStatus string
	err := row.Scan(&id, &out.Kind, &oid, &out.MerchantOrderNo, &out.Nickname, &out.Status, &out.Attempts, &out.TotalAttempts, &out.Source, &out.Result, &created, &updated, &run, &lease, &aid, &out.ActorName, &out.RequestID, &recording, &orderStatus)
	out.ID = storage.IDString(id)
	out.OrderID = storage.IDString(oid)
	out.ActorID = storage.IDString(aid)
	out.CreatedAt = time.Unix(created, 0).UTC()
	out.UpdatedAt = time.Unix(updated, 0).UTC()
	out.RunAt = time.Unix(run, 0).UTC()
	out.RecordingSince = time.Unix(recording, 0).UTC()
	if lease.Valid {
		v := time.Unix(lease.Int64, 0).UTC()
		out.LeaseUntil = &v
	}
	out.Result = safeTaskText(out.Result)
	out.Retryable = (out.Status == "failed" || out.Status == "ready") && (out.Kind == "geo" || orderStatus != "paid")
	return out, err
}
func (s *Service) Tasks(ctx context.Context, f TaskFilter) (Page[TaskItem], error) {
	out := Page[TaskItem]{Items: []TaskItem{}, Page: max(1, f.Page), Size: f.Size}
	if out.Size == 0 {
		out.Size = 20
	}
	where, args, err := taskWhere(f)
	if err != nil {
		return out, err
	}
	tx, err := s.Store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT count(*)"+taskJoins+" WHERE "+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	col := taskSorts[f.Sort]
	if col == "" {
		col = "j.created_at"
	}
	direction := " DESC"
	if f.Direction == "asc" {
		direction = " ASC"
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+taskColumns+taskJoins+" WHERE "+where+" ORDER BY "+col+direction+",j.id"+direction+" LIMIT ? OFFSET ?", append(args, out.Size, (out.Page-1)*out.Size)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanTask(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		if v.Kind == "geo" && s.Config.GeoMode == "disabled" {
			v.Retryable = false
		}
		out.Items = append(out.Items, v.TaskItem)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (s *Service) Task(ctx context.Context, id string) (TaskDetail, error) {
	bid, err := storage.ParseID(id)
	if err != nil {
		return TaskDetail{}, auth.ErrInvalid
	}
	out, err := scanTask(s.Store.DB.QueryRowContext(ctx, "SELECT "+taskColumns+taskJoins+" WHERE j.id=?", bid))
	if out.Kind == "geo" && s.Config.GeoMode == "disabled" {
		out.Retryable = false
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = order.ErrNotFound
	}
	return out, err
}
func (s *Service) TaskHistory(ctx context.Context, id string, page, size int) (Page[TaskEvent], error) {
	out := Page[TaskEvent]{Items: []TaskEvent{}, Page: page, Size: size}
	bid, err := storage.ParseID(id)
	if err != nil || page < 1 || page > 1000000 || size < 1 || size > 100 {
		return out, auth.ErrInvalid
	}
	tx, err := s.Store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE id=?", bid).Scan(&exists); err != nil {
		return out, err
	}
	if exists == 0 {
		return out, order.ErrNotFound
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM task_events WHERE job_id=?", bid).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT e.id,e.event,e.status,e.attempt,e.source,e.result,e.message,e.created_at,e.run_at,e.duration_ms,e.actor_id,COALESCE(i.nickname,''),e.request_id FROM task_events e LEFT JOIN users u ON u.id=e.actor_id LEFT JOIN identities i ON i.id=u.public_identity_id WHERE e.job_id=? ORDER BY e.sequence DESC LIMIT ? OFFSET ?", bid, size, (page-1)*size)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v TaskEvent
		var eid, aid []byte
		var at int64
		var run, duration sql.NullInt64
		if err = rows.Scan(&eid, &v.Event, &v.Status, &v.Attempt, &v.Source, &v.Result, &v.Message, &at, &run, &duration, &aid, &v.ActorName, &v.RequestID); err != nil {
			rows.Close()
			return out, err
		}
		v.ID = storage.IDString(eid)
		v.ActorID = storage.IDString(aid)
		v.CreatedAt = time.Unix(at, 0).UTC()
		if run.Valid {
			r := time.Unix(run.Int64, 0).UTC()
			v.RunAt = &r
		}
		if duration.Valid {
			d := duration.Int64
			v.DurationMS = &d
		}
		v.Result = safeTaskText(v.Result)
		v.Message = safeTaskText(v.Message)
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
