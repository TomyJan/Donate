package order

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/TomyJan/Donate/internal/storage"
)

var (
	ErrInvalid    = errors.New("订单参数无效")
	ErrNotFound   = errors.New("订单不存在")
	ErrConflict   = errors.New("重复请求参数不同")
	ErrForbidden  = errors.New("无权访问订单")
	ErrNotPayable = errors.New("订单不可继续支付")
	ErrBusy       = errors.New("操作正在进行")
	ErrMismatch   = errors.New("支付结果与订单不一致")
)

type Service struct {
	Store                           *storage.Store
	Log                             *slog.Logger
	GatewayID, MerchantID, Protocol string
	Now                             func() time.Time
}

func New(s *storage.Store, l *slog.Logger, gateway, merchant, protocol string) *Service {
	return &Service{s, l, strings.TrimRight(gateway, "/"), merchant, protocol, time.Now}
}

type Order struct {
	ID                string     `json:"id"`
	UserID            string     `json:"user_id"`
	MerchantOrderNo   string     `json:"merchant_order_no"`
	AmountFen         int64      `json:"amount_fen"`
	Channel           string     `json:"channel"`
	Message           string     `json:"message"`
	Status            string     `json:"status"`
	CloseReason       string     `json:"close_reason"`
	CreatedAt         time.Time  `json:"created_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
	ClosedAt          *time.Time `json:"closed_at,omitempty"`
	Protocol          string     `json:"protocol"`
	MerchantID        string     `json:"-"`
	GatewayID         string     `json:"-"`
	TradeNo           string     `json:"trade_no"`
	APITradeNo        string     `json:"api_trade_no"`
	GatewayStatus     string     `json:"gateway_status"`
	LastCheckedAt     *time.Time `json:"last_checked_at,omitempty"`
	RemoteCloseStatus string     `json:"remote_close_status"`
	HiddenOrder       bool       `json:"hidden_order"`
	HiddenMessage     bool       `json:"hidden_message"`
	IP                string     `json:"-"`
	SubmissionStatus  string     `json:"-"`
	CheckoutURL       string     `json:"-"`
	DonorNickname     string     `json:"-"`
	DonorAvatarURL    string     `json:"-"`
	DonorQQNumber     string     `json:"-"`
}

const Columns = "id,user_id,merchant_order_no,amount_fen,channel,message,status,close_reason,created_at,expires_at,paid_at,closed_at,protocol,merchant_id,gateway_id,trade_no,api_trade_no,gateway_status,last_checked_at,remote_close_status,hidden_order,hidden_message,request_ip,submission_status,checkout_url,donor_nickname,donor_avatar_url,donor_qq_number"

type scanner interface{ Scan(...any) error }

func Scan(row scanner) (Order, error) {
	var v Order
	var id, uid []byte
	var created, expires int64
	var paid, closed, checked sql.NullInt64
	var trade sql.NullString
	e := row.Scan(&id, &uid, &v.MerchantOrderNo, &v.AmountFen, &v.Channel, &v.Message, &v.Status, &v.CloseReason, &created, &expires, &paid, &closed, &v.Protocol, &v.MerchantID, &v.GatewayID, &trade, &v.APITradeNo, &v.GatewayStatus, &checked, &v.RemoteCloseStatus, &v.HiddenOrder, &v.HiddenMessage, &v.IP, &v.SubmissionStatus, &v.CheckoutURL, &v.DonorNickname, &v.DonorAvatarURL, &v.DonorQQNumber)
	v.ID, v.UserID = storage.IDString(id), storage.IDString(uid)
	v.CreatedAt = time.Unix(created, 0).UTC()
	v.ExpiresAt = time.Unix(expires, 0).UTC()
	v.TradeNo = trade.String
	if paid.Valid {
		t := time.Unix(paid.Int64, 0).UTC()
		v.PaidAt = &t
	}
	if closed.Valid {
		t := time.Unix(closed.Int64, 0).UTC()
		v.ClosedAt = &t
	}
	if checked.Valid {
		t := time.Unix(checked.Int64, 0).UTC()
		v.LastCheckedAt = &t
	}
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return v, e
}
func (s *Service) Get(ctx context.Context, id string) (Order, error) {
	b, e := storage.ParseID(id)
	if e != nil {
		return Order{}, ErrNotFound
	}
	return Scan(s.Store.DB.QueryRowContext(ctx, "SELECT "+Columns+" FROM orders WHERE id=?", b))
}

type CreateInput struct {
	UserID                    string
	AmountFen                 int64
	Channel, Message, IP, Key string
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Order, error) {
	uid, e := storage.ParseID(in.UserID)
	if e != nil {
		return Order{}, ErrForbidden
	}
	if in.AmountFen < 1 || in.AmountFen > 10000000 || in.Channel != "alipay" && in.Channel != "wxpay" || !utf8.ValidString(in.Message) || utf8.RuneCountInString(in.Message) > 500 || len(in.Key) < 1 || len(in.Key) > 128 {
		return Order{}, ErrInvalid
	}
	raw, _ := json.Marshal([]any{in.AmountFen, in.Channel, in.Message})
	digest := sha256.Sum256(raw)
	id := storage.NewID()
	now := s.Now().UTC()
	var existing []byte
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		var previous []byte
		e := tx.QueryRowContext(ctx, "SELECT digest,target_id FROM idempotency_records WHERE caller_id=? AND action='order.create' AND key=?", uid, in.Key).Scan(&previous, &existing)
		if e == nil {
			if string(previous) != string(digest[:]) {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var status string
		if e = tx.QueryRowContext(ctx, "SELECT status FROM users WHERE id=?", uid).Scan(&status); e != nil || status != "active" {
			return ErrForbidden
		}
		var recent, pending int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM orders WHERE user_id=? AND created_at>?", uid, now.Add(-time.Minute).Unix()).Scan(&recent); e != nil {
			return e
		}
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM orders WHERE user_id=? AND status='pending' AND expires_at>?", uid, now.Unix()).Scan(&pending); e != nil {
			return e
		}
		if recent >= 5 || pending >= 10 {
			return ErrBusy
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO orders(id,user_id,merchant_order_no,amount_fen,channel,message,created_at,expires_at,protocol,merchant_id,gateway_id,request_ip) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id[:], uid, strings.ReplaceAll(id.String(), "-", ""), in.AmountFen, in.Channel, in.Message, now.Unix(), now.Add(30*time.Minute).Unix(), s.Protocol, s.MerchantID, s.GatewayID, in.IP)
		if e != nil {
			return e
		}
		for _, kind := range []string{"payment", "geo"} {
			job := storage.NewID()
			run := now.Unix()
			if kind == "payment" {
				run = now.Add(time.Minute).Unix()
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO jobs(id,kind,order_id,run_at,created_at,updated_at) VALUES(?,?,?,?,?,?)", job[:], kind, id[:], run, now.Unix(), now.Unix()); e != nil {
				return e
			}
		}
		receipt := storage.NewID()
		_, e = tx.ExecContext(ctx, "INSERT INTO idempotency_records(id,caller_id,action,key,digest,target_id,created_at,expires_at) VALUES(?,?,'order.create',?,?,?,?,?)", receipt[:], uid, in.Key, digest[:], id[:], now.Unix(), now.Add(24*time.Hour).Unix())
		if e != nil {
			return e
		}
		return storage.Audit(ctx, tx, uid, "order.create", "order", id[:], "created", now.Unix())
	})
	if e != nil {
		return Order{}, e
	}
	if len(existing) > 0 {
		return s.Get(ctx, storage.IDString(existing))
	}
	v, e := s.Get(ctx, id.String())
	if e == nil {
		s.Log.Info(fmt.Sprintf("用户 %s 创建订单 %s，金额 %d 分，渠道 %s", in.UserID, v.ID, in.AmountFen, in.Channel), "order_id", v.ID, "user_id", in.UserID, "amount_fen", in.AmountFen, "channel", in.Channel)
	}
	return v, e
}
func (s *Service) Expire(ctx context.Context) error {
	return s.Store.Write(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, "UPDATE orders SET status='closed',close_reason='expired',closed_at=?,version=version+1 WHERE status='pending' AND expires_at<=? RETURNING id", s.Now().Unix(), s.Now().Unix())
		if e != nil {
			return e
		}
		var ids [][]byte
		for rows.Next() {
			var id []byte
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return e
		}
		rows.Close()
		for _, id := range ids {
			if e = storage.Audit(ctx, tx, nil, "order.expire", "order", id, "closed", s.Now().Unix()); e != nil {
				return e
			}
			s.Log.Info(fmt.Sprintf("订单 %s 已到期，停止本站继续支付，渠道结果仍待核实", storage.IDString(id)), "order_id", storage.IDString(id))
		}
		return nil
	})
}
func (s *Service) Delete(ctx context.Context, id, actor string) error {
	bid, e := storage.ParseID(id)
	if e != nil {
		return ErrNotFound
	}
	aid, _ := storage.ParseID(actor)
	e = s.Store.Write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "DELETE FROM orders WHERE id=?", bid)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrNotFound
		}
		if _, e = tx.ExecContext(ctx, "UPDATE admin_operation_items SET result='deleted',job_id=NULL WHERE target_id=?", bid); e != nil {
			return e
		}
		return storage.Audit(ctx, tx, aid, "order.delete", "order", bid, "deleted", s.Now().Unix())
	})
	if e == nil {
		s.Log.Info(fmt.Sprintf("管理员 %s 已物理删除订单 %s 及关联任务", actor, id), "order_id", id, "actor_id", actor)
	}
	return e
}
