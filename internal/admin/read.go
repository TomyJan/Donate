package admin

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
	"io"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

type Filter struct {
	Search, Status, Channel, Visibility, From, To, Sort, Direction, Action, UserID string
	Page, Size                                                                     int
	Min, Max                                                                       *int64
}

func validSearch(search string) bool {
	return len(search) <= 128*utf8.UTFMax && utf8.ValidString(search) && utf8.RuneCountInString(search) <= 128
}

type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Size  int `json:"page_size"`
}

func (f Filter) pagination() (int, int) { return max(f.Page, 1), min(max(f.Size, 1), 100) }
func sortSQL(f Filter, allowed map[string]string, defaultCol string) string {
	col := allowed[f.Sort]
	if col == "" {
		col = defaultCol
	}
	dir := " DESC"
	if f.Direction == "asc" {
		dir = " ASC"
	}
	return col + dir + ",id" + dir
}

type OrderRow struct {
	order.Order
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

func (s *Service) OrdersList(ctx context.Context, f Filter) (Page[OrderRow], error) {
	page, size := f.pagination()
	out := Page[OrderRow]{Items: []OrderRow{}, Page: page, Size: size}
	where, args, e := orderWhere(f)
	if e != nil {
		return out, e
	}
	if e = s.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM orders o LEFT JOIN users u ON u.id=o.user_id LEFT JOIN identities i ON i.id=u.public_identity_id WHERE "+where, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	allowed := map[string]string{"id": "o.id", "created_at": "o.created_at", "nickname": "COALESCE(NULLIF(trim(o.donor_nickname, ' '||char(9)||char(10)||char(13)||char(12288)),''),i.nickname)", "amount_fen": "o.amount_fen", "channel": "o.channel", "message": "o.message", "status": "o.status", "hidden_order": "o.hidden_order", "hidden_message": "o.hidden_message", "paid_at": "o.paid_at"}
	col := allowed[f.Sort]
	if col == "" {
		col = "o.created_at"
	}
	dir := " DESC"
	if f.Direction == "asc" {
		dir = " ASC"
	}
	// Read IDs first and close rows before looking up orders: SQLite uses one bounded connection.
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT o.id,COALESCE(NULLIF(trim(o.donor_nickname, ' '||char(9)||char(10)||char(13)||char(12288)),''),i.nickname,'捐赠者'),COALESCE(NULLIF(o.donor_avatar_url,''),i.avatar_url,'') FROM orders o LEFT JOIN users u ON u.id=o.user_id LEFT JOIN identities i ON i.id=u.public_identity_id WHERE "+where+" ORDER BY "+col+dir+",o.id"+dir+" LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var r OrderRow
		var id []byte
		if e = rows.Scan(&id, &r.Nickname, &r.AvatarURL); e != nil {
			rows.Close()
			return out, e
		}
		r.ID = storage.IDString(id)
		out.Items = append(out.Items, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for i := range out.Items {
		o, e := s.Orders.Get(ctx, out.Items[i].ID)
		if errors.Is(e, order.ErrNotFound) {
			continue
		}
		if e != nil {
			return out, e
		}
		out.Items[i].Order = o
	}
	return out, nil
}
func orderWhere(f Filter) (string, []any, error) {
	where := "1=1"
	args := []any{}
	if f.UserID != "" {
		uid, e := storage.ParseID(f.UserID)
		if e != nil {
			return "", nil, auth.ErrInvalid
		}
		where += " AND o.user_id=?"
		args = append(args, uid)
	}
	if !validSearch(f.Search) {
		return "", nil, auth.ErrInvalid
	}
	if f.Search != "" {
		where += " AND (lower(COALESCE(NULLIF(trim(o.donor_nickname, ' '||char(9)||char(10)||char(13)||char(12288)),''),i.nickname)) LIKE ? ESCAPE '\\' OR lower(hex(o.id)) LIKE ? ESCAPE '\\' OR o.merchant_order_no LIKE ? ESCAPE '\\' OR lower(hex(o.user_id)) LIKE ? ESCAPE '\\')"
		// Preserve Unicode nickname spelling; SQLite LIKE/lower only fold ASCII.
		q := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(f.Search)
		lowerq := strings.ToLower(q)
		hexq := strings.ReplaceAll(lowerq, "-", "")
		args = append(args, "%"+q+"%", "%"+hexq+"%", "%"+lowerq+"%", "%"+hexq+"%")
	}
	if f.Status != "" {
		if f.Status != "paid" && f.Status != "pending" && f.Status != "closed" {
			return "", nil, auth.ErrInvalid
		}
		where += " AND o.status=?"
		args = append(args, f.Status)
	}
	if f.Channel != "" {
		if f.Channel != "alipay" && f.Channel != "wxpay" && f.Channel != "qqpay" {
			return "", nil, auth.ErrInvalid
		}
		where += " AND o.channel=?"
		args = append(args, f.Channel)
	}
	switch f.Visibility {
	case "":
	case "public":
		where += " AND o.hidden_order=0 AND o.hidden_message=0"
	case "hidden_order":
		where += " AND o.hidden_order=1"
	case "hidden_message":
		where += " AND o.hidden_message=1"
	default:
		return "", nil, auth.ErrInvalid
	}
	var from, to time.Time
	for _, p := range []struct {
		value, operator string
		target          *time.Time
	}{{f.From, ">=", &from}, {f.To, "<", &to}} {
		if p.value != "" {
			at, e := time.Parse(time.RFC3339, p.value)
			if e != nil {
				return "", nil, auth.ErrInvalid
			}
			*p.target = at
			where += " AND o.created_at" + p.operator + "?"
			// Stored seconds satisfy exact >=from and <to bounds when both
			// fractional RFC3339 instants are rounded up to the next second.
			second := at.Unix()
			if at.Nanosecond() != 0 {
				second++
			}
			args = append(args, second)
		}
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return "", nil, auth.ErrInvalid
	}
	if f.Min != nil {
		where += " AND o.amount_fen>=?"
		args = append(args, *f.Min)
	}
	if f.Max != nil {
		where += " AND o.amount_fen<=?"
		args = append(args, *f.Max)
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		return "", nil, auth.ErrInvalid
	}
	return where, args, nil
}

type Detail struct {
	Province        string               `json:"province"`
	Reconciliations []ReconciliationView `json:"reconciliations,omitempty"`
	order.Order
	IP            string        `json:"ip,omitempty"`
	QQNumber      string        `json:"qq_number,omitempty"`
	Nickname      string        `json:"nickname"`
	AvatarURL     string        `json:"avatar_url"`
	Location      *geo.Location `json:"location,omitempty"`
	LocationCheck *geo.Location `json:"location_check,omitempty"`
	Jobs          []JobView     `json:"jobs,omitempty"`
}
type JobView struct {
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Result    string    `json:"result"`
	Attempts  int       `json:"attempts"`
	RunAt     time.Time `json:"run_at"`
}

// ReconciliationView is a persisted manual request, even when requests share a job.
// Status and result describe the currently linked job, not an independent execution.
type ReconciliationView struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
	Result    string    `json:"result"`
}

func (s *Service) Detail(ctx context.Context, id string, full bool) (Detail, error) {
	o, e := s.Orders.Get(ctx, id)
	if e != nil {
		return Detail{}, e
	}
	d := Detail{Order: o, Nickname: o.DonorNickname, AvatarURL: o.DonorAvatarURL}
	oid, _ := storage.ParseID(id)
	e = s.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(NULLIF(trim(o.donor_nickname, ' '||char(9)||char(10)||char(13)||char(12288)),''),i.nickname,'捐赠者'),COALESCE(NULLIF(o.donor_avatar_url,''),i.avatar_url,'') FROM orders o LEFT JOIN users u ON u.id=o.user_id LEFT JOIN identities i ON i.id=u.public_identity_id WHERE o.id=?", oid).Scan(&d.Nickname, &d.AvatarURL)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return d, e
	}
	p := geo.Location{}
	var at int64
	e = s.Store.DB.QueryRowContext(ctx, "SELECT country,province,city,district,street,isp,asn,latitude,longitude,accuracy_radius,source,source_version,queried_at FROM order_locations WHERE order_id=?", oid).Scan(&p.Country, &p.Province, &p.City, &p.District, &p.Street, &p.ISP, &p.ASN, &p.Latitude, &p.Longitude, &p.AccuracyRadius, &p.Source, &p.Version, &at)
	if e == nil {
		p.QueriedAt = time.Unix(at, 0).UTC()
		d.Province = p.Province
	} else if !errors.Is(e, sql.ErrNoRows) {
		return d, e
	}
	if !full {
		return d, nil
	}
	d.IP = o.IP
	d.QQNumber = o.DonorQQNumber
	if e == nil {
		d.Location = &p
	}
	var checkJSON string
	checkErr := s.Store.DB.QueryRowContext(ctx, "SELECT result_json FROM order_location_checks WHERE order_id=?", oid).Scan(&checkJSON)
	if checkErr == nil {
		var check geo.Location
		if err := json.Unmarshal([]byte(checkJSON), &check); err != nil {
			return d, err
		}
		d.LocationCheck = &check
	} else if !errors.Is(checkErr, sql.ErrNoRows) {
		return d, checkErr
	}
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT id,kind,status,last_result,attempts,run_at,source,created_at FROM jobs WHERE order_id=? ORDER BY created_at DESC,id DESC", oid)
	if e != nil {
		return d, e
	}
	defer rows.Close()
	for rows.Next() {
		var j JobView
		var id []byte
		var at, created int64
		if e = rows.Scan(&id, &j.Kind, &j.Status, &j.Result, &j.Attempts, &at, &j.Source, &created); e != nil {
			return d, e
		}
		j.ID = storage.IDString(id)
		j.RunAt = time.Unix(at, 0).UTC()
		j.CreatedAt = time.Unix(created, 0).UTC()
		d.Jobs = append(d.Jobs, j)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	// The request UUID is persisted independently of its idempotency key. Join through
	// the live job and order so deleted targets cannot expose retained request data.
	requests, e := s.Store.DB.QueryContext(ctx, "SELECT r.id,j.id,r.created_at,j.status,j.last_result FROM idempotency_records r JOIN jobs j ON j.id=r.target_id JOIN orders o ON o.id=j.order_id WHERE r.action='order.reconcile' AND j.kind='payment' AND o.id=? ORDER BY r.created_at DESC,r.id DESC", oid)
	if e != nil {
		return d, e
	}
	defer requests.Close()
	for requests.Next() {
		var request ReconciliationView
		var id, job []byte
		var created int64
		if e = requests.Scan(&id, &job, &created, &request.Status, &request.Result); e != nil {
			return d, e
		}
		request.ID, request.JobID = storage.IDString(id), storage.IDString(job)
		request.CreatedAt = time.Unix(created, 0).UTC()
		d.Reconciliations = append(d.Reconciliations, request)
	}
	return d, requests.Err()
}

type User struct {
	ID         string          `json:"id"`
	Nickname   string          `json:"nickname"`
	AvatarURL  string          `json:"avatar_url"`
	Status     string          `json:"status"`
	Admin      bool            `json:"admin"`
	CreatedAt  time.Time       `json:"created_at"`
	LastLogin  *time.Time      `json:"last_login_at,omitempty"`
	Bindings   []auth.Identity `json:"bindings"`
	OrderCount int             `json:"order_count"`
	PaidFen    int64           `json:"paid_fen"`
}

func (s *Service) Users(ctx context.Context, f Filter) (Page[User], error) {
	page, size := f.pagination()
	out := Page[User]{Items: []User{}, Page: page, Size: size}
	where := "1=1"
	args := []any{}
	if f.Search != "" {
		if !validSearch(f.Search) {
			return out, auth.ErrInvalid
		}
		q := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(f.Search)
		where += " AND (i.nickname LIKE ? ESCAPE '\\' OR lower(hex(u.id)) LIKE ? ESCAPE '\\')"
		args = append(args, "%"+q+"%", "%"+strings.ReplaceAll(strings.ToLower(q), "-", "")+"%")
	}
	if f.Status != "" {
		if f.Status != "active" && f.Status != "banned" {
			return out, auth.ErrInvalid
		}
		where += " AND u.status=?"
		args = append(args, f.Status)
	}
	if e := s.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM users u LEFT JOIN identities i ON i.id=u.public_identity_id WHERE "+where, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	col := map[string]string{"id": "u.id", "nickname": "i.nickname", "status": "u.status", "created_at": "u.created_at", "last_login_at": "u.last_login_at", "bindings": "(SELECT count(*) FROM identities b WHERE b.user_id=u.id)", "order_count": "(SELECT count(*) FROM orders o WHERE o.user_id=u.id)", "paid_fen": "(SELECT COALESCE(sum(amount_fen),0) FROM orders o WHERE o.user_id=u.id AND o.status='paid')"}[f.Sort]
	if col == "" {
		col = "u.created_at"
	}
	dir := " DESC"
	if f.Direction == "asc" {
		dir = " ASC"
	}
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT u.id FROM users u LEFT JOIN identities i ON i.id=u.public_identity_id WHERE "+where+" ORDER BY "+col+dir+",u.id"+dir+" LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	if e != nil {
		return out, e
	}
	var ids []string
	for rows.Next() {
		var id []byte
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, storage.IDString(id))
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, id := range ids {
		u, e := s.User(ctx, id)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, u)
	}
	return out, nil
}
func (s *Service) User(ctx context.Context, id string) (User, error) {
	out := User{ID: id, Admin: s.Config.AdminIDs[id], Bindings: []auth.Identity{}}
	uid, e := storage.ParseID(id)
	if e != nil {
		return out, auth.ErrInvalid
	}
	var created int64
	var last sql.NullInt64
	e = s.Store.DB.QueryRowContext(ctx, "SELECT u.status,u.created_at,u.last_login_at,COALESCE(i.nickname,''),COALESCE(i.avatar_url,''),(SELECT count(*) FROM orders o WHERE o.user_id=u.id),(SELECT COALESCE(sum(amount_fen),0) FROM orders o WHERE o.user_id=u.id AND status='paid') FROM users u LEFT JOIN identities i ON i.id=u.public_identity_id WHERE u.id=?", uid).Scan(&out.Status, &created, &last, &out.Nickname, &out.AvatarURL, &out.OrderCount, &out.PaidFen)
	if e != nil {
		return out, e
	}
	out.CreatedAt = time.Unix(created, 0).UTC()
	if last.Valid {
		at := time.Unix(last.Int64, 0).UTC()
		out.LastLogin = &at
	}
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT id,provider,nickname,avatar_url,created_at FROM identities WHERE user_id=? ORDER BY provider", uid)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var p auth.Identity
		var id []byte
		var bound int64
		if e = rows.Scan(&id, &p.Provider, &p.Nickname, &p.AvatarURL, &bound); e != nil {
			return out, e
		}
		p.ID = storage.IDString(id)
		p.BoundAt = time.Unix(bound, 0).UTC()
		out.Bindings = append(out.Bindings, p)
	}
	return out, rows.Err()
}

type Metric struct {
	Label   string `json:"label"`
	PaidFen int64  `json:"paid_fen"`
	Count   int    `json:"count"`
}
type Overview struct {
	PaidFen  int64    `json:"paid_fen"`
	Paid     int      `json:"paid"`
	Pending  int      `json:"pending"`
	Closed   int      `json:"closed"`
	Daily    []Metric `json:"daily"`
	Channels []Metric `json:"channels"`
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	return s.OverviewRange(ctx, "30d", "", "")
}

// overviewWindow resolves calendar presets in Shanghai and exact custom instants.
// A nil bound represents the unbounded all-time window.
func overviewWindow(name, from, to string, now time.Time) (*time.Time, *time.Time, error) {
	if name == "" {
		name = "30d"
	}
	if name != "custom" && (from != "" || to != "") {
		return nil, nil, auth.ErrInvalid
	}
	if name == "custom" {
		start, e := time.Parse(time.RFC3339, from)
		if e != nil {
			return nil, nil, auth.ErrInvalid
		}
		end, e := time.Parse(time.RFC3339, to)
		if e != nil || !start.Before(end) {
			return nil, nil, auth.ErrInvalid
		}
		return &start, &end, nil
	}
	if name == "all" {
		return nil, nil, nil
	}
	zone, e := time.LoadLocation("Asia/Shanghai")
	if e != nil {
		return nil, nil, e
	}
	local := now.In(zone)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	start, end := today, today.AddDate(0, 0, 1)
	switch name {
	case "today":
	case "yesterday":
		start, end = today.AddDate(0, 0, -1), today
	case "7d":
		start = today.AddDate(0, 0, -6)
	case "30d":
		start = today.AddDate(0, 0, -29)
	case "90d":
		start = today.AddDate(0, 0, -89)
	default:
		return nil, nil, auth.ErrInvalid
	}
	return &start, &end, nil
}

func (s *Service) OverviewRange(ctx context.Context, name, from, to string) (Overview, error) {
	out := Overview{Daily: []Metric{}, Channels: []Metric{}}
	start, end, e := overviewWindow(name, from, to, s.Now())
	if e != nil {
		return out, e
	}
	// Only fixed internal column names are used; all date bounds are bound parameters.
	window := func(column string) (string, []any) {
		if start == nil {
			return "", nil
		}
		// Storage timestamps have second precision. Ceiling preserves exact custom
		// [from,to) semantics even when RFC3339 values contain fractional seconds.
		ceil := func(at time.Time) int64 {
			second := at.Unix()
			if at.Nanosecond() != 0 {
				second++
			}
			return second
		}
		return " AND " + column + ">=? AND " + column + "<?", []any{ceil(*start), ceil(*end)}
	}
	paidWhere, paidArgs := window("COALESCE(paid_at,created_at)")
	createdWhere, createdArgs := window("created_at")
	// Keep cards and both series in the same read snapshot during concurrent writes.
	tx, e := s.Store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	e = tx.QueryRowContext(ctx, "SELECT COALESCE(sum(amount_fen),0),count(*) FROM orders WHERE status='paid'"+paidWhere, paidArgs...).Scan(&out.PaidFen, &out.Paid)
	if e != nil {
		return out, e
	}
	e = tx.QueryRowContext(ctx, "SELECT count(CASE WHEN status='pending' THEN 1 END),count(CASE WHEN status='closed' THEN 1 END) FROM orders WHERE status IN ('pending','closed')"+createdWhere, createdArgs...).Scan(&out.Pending, &out.Closed)
	if e != nil {
		return out, e
	}
	for _, item := range []struct {
		expr   string
		target *[]Metric
	}{{"strftime('%Y-%m-%d',COALESCE(paid_at,created_at),'unixepoch','+8 hours')", &out.Daily}, {"channel", &out.Channels}} {
		rows, e := tx.QueryContext(ctx, "SELECT "+item.expr+",sum(amount_fen),count(*) FROM orders WHERE status='paid'"+paidWhere+" GROUP BY "+item.expr+" ORDER BY "+item.expr, paidArgs...)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var m Metric
			if e = rows.Scan(&m.Label, &m.PaidFen, &m.Count); e != nil {
				rows.Close()
				return out, e
			}
			*item.target = append(*item.target, m)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
	}
	return out, tx.Commit()
}

type Audit struct {
	ID              string    `json:"id"`
	ActorID         string    `json:"actor_id"`
	Actor           string    `json:"actor"`
	Action          string    `json:"action"`
	TargetType      string    `json:"target_type"`
	TargetID        string    `json:"target_id"`
	Target          string    `json:"target"`
	TargetExists    bool      `json:"target_exists"`
	TargetNickname  string    `json:"target_nickname,omitempty"`
	TargetAmountFen *int64    `json:"target_amount_fen,omitempty"`
	Result          string    `json:"result"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Service) Audit(ctx context.Context, f Filter) (Page[Audit], error) {
	page, size := f.pagination()
	out := Page[Audit]{Items: []Audit{}, Page: page, Size: size}
	where := "1=1"
	args := []any{}
	if f.Action != "" {
		where = "a.action LIKE ?"
		args = append(args, f.Action+"%")
	}
	if e := s.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM audit_events a WHERE "+where, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	col := map[string]string{"created_at": "a.created_at", "id": "a.id", "actor": "i.nickname", "action": "a.action", "target": "COALESCE(NULLIF(trim(o.donor_nickname, ' '||char(9)||char(10)||char(13)||char(12288)),''),oi.nickname,ti.nickname,'')", "result": "a.result"}[f.Sort]
	if col == "" {
		col = "a.created_at"
	}
	dir := " DESC"
	if f.Direction == "asc" {
		dir = " ASC"
	}
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT a.id,a.actor_id,COALESCE(NULLIF(i.nickname,''),'system'),a.action,a.target_type,a.target_id,CASE WHEN a.target_type='order' THEN o.id IS NOT NULL WHEN a.target_type='user' THEN tu.id IS NOT NULL ELSE 0 END,CASE WHEN a.target_type='order' THEN COALESCE(NULLIF(trim(o.donor_nickname, ' '||char(9)||char(10)||char(13)||char(12288)),''),oi.nickname,'') ELSE COALESCE(ti.nickname,'') END,o.amount_fen,a.result,a.created_at FROM audit_events a LEFT JOIN users u ON u.id=a.actor_id LEFT JOIN identities i ON i.id=u.public_identity_id LEFT JOIN orders o ON a.target_type='order' AND o.id=a.target_id LEFT JOIN users ou ON ou.id=o.user_id LEFT JOIN identities oi ON oi.id=ou.public_identity_id LEFT JOIN users tu ON a.target_type='user' AND tu.id=a.target_id LEFT JOIN identities ti ON ti.id=tu.public_identity_id WHERE "+where+" ORDER BY "+col+dir+",a.id"+dir+" LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var a Audit
		var id, actor, target []byte
		var at int64
		var amount sql.NullInt64
		if e = rows.Scan(&id, &actor, &a.Actor, &a.Action, &a.TargetType, &target, &a.TargetExists, &a.TargetNickname, &amount, &a.Result, &at); e != nil {
			return out, e
		}
		if a.TargetExists {
			a.Target = a.TargetNickname
			if a.TargetType == "order" && amount.Valid {
				a.TargetAmountFen = &amount.Int64
				price := fmt.Sprintf("¥%d.%02d", amount.Int64/100, amount.Int64%100)
				if a.Target == "" {
					a.Target = price
				} else {
					a.Target += " · " + price
				}
			}
		}
		a.ID = storage.IDString(id)
		a.ActorID = storage.IDString(actor)
		a.TargetID = storage.IDString(target)
		a.CreatedAt = time.Unix(at, 0).UTC()
		out.Items = append(out.Items, a)
	}
	return out, rows.Err()
}
func csvSafe(v string) string {
	if strings.ContainsAny(strings.TrimLeft(v, " \t\r\n")[:min(len(strings.TrimLeft(v, " \t\r\n")), 1)], "=+-@") || strings.HasPrefix(v, "\t") || strings.HasPrefix(v, "\r") {
		return "'" + v
	}
	return v
}
func (s *Service) Export(ctx context.Context, actor, operation string, w io.Writer) error {
	op, e := s.Operation(ctx, actor, operation)
	if e != nil {
		return e
	}
	if op.Action != "export" || !op.Confirmed {
		return auth.ErrInvalid
	}
	cw := csv.NewWriter(w)
	if e = cw.Write([]string{"ID", "用户 ID", "商户订单号", "金额（分）", "渠道", "状态", "创建时间", "留言"}); e != nil {
		return e
	}
	for _, item := range op.Items {
		if item.Result != "exported" {
			continue
		}
		o, e := s.Orders.Get(ctx, item.ID)
		if errors.Is(e, order.ErrNotFound) {
			continue
		}
		if e != nil {
			return e
		}
		values := []string{o.ID, o.UserID, o.MerchantOrderNo, strconv.FormatInt(o.AmountFen, 10), o.Channel, o.Status, o.CreatedAt.Format(time.RFC3339), o.Message}
		for i := range values {
			values[i] = csvSafe(values[i])
		}
		if e = cw.Write(values); e != nil {
			return e
		}
	}
	cw.Flush()
	if e = cw.Error(); e != nil {
		return fmt.Errorf("CSV 写入失败: %w", e)
	}
	return nil
}
