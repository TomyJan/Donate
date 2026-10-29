package order

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/TomyJan/Donate/internal/storage"
	"strings"
	"time"
)

type Record struct {
	ID        string    `json:"id"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url"`
	AmountFen int64     `json:"amount_fen"`
	Message   string    `json:"message"`
	Province  string    `json:"province"`
	Date      time.Time `json:"date"`
	Status    string    `json:"status"`
}
type Page struct {
	Items []Record `json:"items"`
	Next  string   `json:"next_cursor"`
}
type cursor struct {
	At   int64
	ID   string
	Mine bool
}

func (s *Service) List(ctx context.Context, userID string, mine bool, after string) (Page, error) {
	out := Page{Items: []Record{}}
	where := "o.status='paid' AND o.hidden_order=0"
	date := "o.paid_at"
	args := []any{}
	if mine {
		uid, e := storage.ParseID(userID)
		if e != nil {
			return out, ErrForbidden
		}
		where = "o.user_id=?"
		date = "o.created_at"
		args = append(args, uid)
	}
	if userID != "" && after != "" {
		if len(after) > 512 {
			return out, ErrInvalid
		}
		b, e := base64.RawURLEncoding.DecodeString(after)
		var c cursor
		if e != nil || json.Unmarshal(b, &c) != nil || c.Mine != mine {
			return out, ErrInvalid
		}
		id, e := storage.ParseID(c.ID)
		if e != nil {
			return out, ErrInvalid
		}
		where += " AND (" + date + "<? OR (" + date + "=? AND o.id<?))"
		args = append(args, c.At, c.At, id)
	}
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT o.id,COALESCE(i.nickname,'捐赠者'),COALESCE(i.avatar_url,''),o.amount_fen,CASE WHEN ? OR o.hidden_message=0 THEN o.message ELSE '' END,COALESCE(l.province,''),"+date+",o.status FROM orders o LEFT JOIN users u ON u.id=o.user_id LEFT JOIN identities i ON i.id=u.public_identity_id LEFT JOIN order_locations l ON l.order_id=o.id WHERE "+where+" ORDER BY "+date+" DESC,o.id DESC LIMIT 11", append([]any{mine}, args...)...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var r Record
		var id []byte
		var at int64
		if e = rows.Scan(&id, &r.Nickname, &r.AvatarURL, &r.AmountFen, &r.Message, &r.Province, &at, &r.Status); e != nil {
			return out, e
		}
		r.ID = storage.IDString(id)
		r.Date = time.Unix(at, 0).UTC()
		out.Items = append(out.Items, r)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(out.Items) > 10 {
		out.Items = out.Items[:10]
		if userID != "" {
			last := out.Items[9]
			b, _ := json.Marshal(cursor{last.Date.Unix(), last.ID, mine})
			out.Next = base64.RawURLEncoding.EncodeToString(b)
		}
	}
	return out, nil
}

// QualifiedColumns is only used with a fixed internal alias, never request input.
func QualifiedColumns(alias string) string {
	cols := strings.Split(Columns, ",")
	for i := range cols {
		cols[i] = alias + "." + cols[i]
	}
	return strings.Join(cols, ",")
}
