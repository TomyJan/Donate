package payment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

type Service struct {
	Orders  *order.Service
	Gateway *Gateway
}

func (s *Service) Start(ctx context.Context, id, userID string) (Checkout, error) {
	o, e := s.Orders.Get(ctx, id)
	if e != nil {
		return Checkout{}, e
	}
	if o.UserID != userID {
		return Checkout{}, order.ErrForbidden
	}
	if o.Status != "pending" || !o.ExpiresAt.After(s.Orders.Now()) {
		return Checkout{}, order.ErrNotPayable
	}
	if e = s.Gateway.account(o); e != nil {
		return Checkout{}, e
	}
	if e = s.payable(ctx, id, userID); e != nil {
		return Checkout{}, e
	}
	if o.Protocol == "v1" {
		checkout, _, e := s.Gateway.Create(ctx, o)
		if e != nil {
			return Checkout{}, e
		}
		bid, _ := storage.ParseID(id)
		uid, _ := storage.ParseID(userID)
		var r sql.Result
		r, e = s.Orders.Store.DB.ExecContext(ctx, "UPDATE orders SET submission_status='submitted' WHERE id=? AND status='pending' AND expires_at>? AND NOT EXISTS(SELECT 1 FROM jobs WHERE order_id=orders.id AND kind='close' AND status IN ('ready','running')) AND EXISTS(SELECT 1 FROM users WHERE id=? AND status='active')", bid, s.Orders.Now().Unix(), uid)
		if e == nil {
			n, _ := r.RowsAffected()
			if n != 1 {
				return Checkout{}, order.ErrNotPayable
			}
		}
		return checkout, e
	}
	if o.CheckoutURL != "" && s.Gateway.validRedirect(o.CheckoutURL) {
		if e = s.payable(ctx, id, userID); e != nil {
			return Checkout{}, e
		}
		return Checkout{Mode: "redirect", URL: o.CheckoutURL}, nil
	}
	bid, _ := storage.ParseID(id)
	uid, _ := storage.ParseID(userID)
	lease := storage.NewID().String()
	now := s.Orders.Now()
	e = s.Orders.Store.Write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "UPDATE orders SET submission_lease=?,submission_lease_until=? WHERE id=? AND user_id=? AND status='pending' AND expires_at>? AND (submission_lease IS NULL OR submission_lease_until<=?) AND NOT EXISTS(SELECT 1 FROM jobs WHERE order_id=orders.id AND kind='close' AND status IN ('ready','running')) AND EXISTS(SELECT 1 FROM users WHERE id=? AND status='active')", lease, now.Add(time.Minute).Unix(), bid, uid, now.Unix(), now.Unix(), uid)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return order.ErrBusy
		}
		// Another start may have completed since the initial read. Keep the
		// current submission state before marking a fresh submission in progress.
		o, e = order.Scan(tx.QueryRowContext(ctx, "SELECT "+order.Columns+" FROM orders WHERE id=?", bid))
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE orders SET submission_status='submitting' WHERE id=? AND submission_status='new'", bid)
		return e
	})
	if e != nil {
		return Checkout{}, e
	}
	release := func(status string) {
		_, err := s.Orders.Store.DB.ExecContext(context.WithoutCancel(ctx), "UPDATE orders SET submission_status=?,submission_lease=NULL,submission_lease_until=NULL WHERE id=? AND submission_lease=?", status, bid, lease)
		if err != nil {
			s.Orders.Log.Error(fmt.Sprintf("订单 %s 的支付提交租约释放失败", id), "order_id", id)
		}
	}
	releaseStatus := o.SubmissionStatus
	defer func() { release(releaseStatus) }()
	if e = s.Gateway.account(o); e != nil {
		return Checkout{}, e
	}
	if e = s.payable(ctx, id, userID); e != nil {
		return Checkout{}, e
	}
	if o.CheckoutURL != "" && s.Gateway.validRedirect(o.CheckoutURL) {
		releaseStatus = "submitted"
		return Checkout{Mode: "redirect", URL: o.CheckoutURL}, nil
	}
	if o.SubmissionStatus != "new" {
		obs, err := s.Gateway.Query(ctx, o)
		if err != nil {
			release("unknown")
			reason := Diagnostic(err)
			s.Orders.Log.Warn(fmt.Sprintf("订单 %s 的支付提交 %s 恢复查单失败：%s，保留原单号等待核验", id, lease, reason), "order_id", id, "request_id", lease, "error", reason)
			return Checkout{}, err
		}
		obs.Source = "submission_recovery"
		obs.RequestID = lease
		if _, err = s.Orders.Apply(ctx, obs); err != nil {
			release("unknown")
			return Checkout{}, err
		}
		if obs.State != "unpaid" {
			release("submitted")
			return Checkout{}, order.ErrNotPayable
		}
	}
	releaseStatus = "unknown"
	checkout, trade, e := s.Gateway.Create(ctx, o)
	if e != nil {
		release("unknown")
		reason := Diagnostic(e)
		s.Orders.Log.Warn(fmt.Sprintf("订单 %s 的支付提交 %s 结果未知：%s，保留原单号等待核验", id, lease, reason), "order_id", id, "request_id", lease, "error", reason)
		return Checkout{}, e
	}
	e = s.Orders.Store.Write(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "UPDATE orders SET checkout_url=?,trade_no=COALESCE(trade_no,?),submission_status='submitted',submission_lease=NULL,submission_lease_until=NULL WHERE id=? AND submission_lease=? AND (trade_no IS NULL OR trade_no=?) AND status='pending' AND expires_at>? AND NOT EXISTS(SELECT 1 FROM jobs WHERE order_id=orders.id AND kind='close' AND status IN ('ready','running')) AND EXISTS(SELECT 1 FROM users WHERE id=? AND status='active')", checkout.URL, trade, bid, lease, trade, s.Orders.Now().Unix(), uid)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return order.ErrNotFound
		}
		return nil
	})
	if e != nil {
		return Checkout{}, e
	}
	s.Orders.Log.Info(fmt.Sprintf("订单 %s 已取得支付跳转，平台流水 %s", id, trade), "order_id", id, "trade_no", trade)
	return checkout, nil
}

func (s *Service) payable(ctx context.Context, id, user string) error {
	bid, _ := storage.ParseID(id)
	uid, _ := storage.ParseID(user)
	var n int
	e := s.Orders.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM orders o JOIN users u ON u.id=o.user_id WHERE o.id=? AND o.user_id=? AND o.status='pending' AND o.expires_at>? AND u.status='active' AND NOT EXISTS(SELECT 1 FROM jobs WHERE order_id=o.id AND kind='close' AND status IN ('ready','running'))", bid, uid, s.Orders.Now().Unix()).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return order.ErrNotPayable
	}
	return nil
}
func (s *Service) Notify(ctx context.Context, values map[string]string, requestID string) (order.Applied, error) {
	no := values["out_trade_no"]
	if no == "" || len(no) > 128 {
		return order.Applied{}, ErrProtocol
	}
	o, e := order.Scan(s.Orders.Store.DB.QueryRowContext(ctx, "SELECT "+order.Columns+" FROM orders WHERE merchant_order_no=?", no))
	if e != nil {
		return order.Applied{}, e
	}
	obs, e := s.Gateway.Notification(o, values)
	if e != nil {
		s.Orders.Log.Warn(fmt.Sprintf("订单 %s 的支付通知 %s 验签或协议校验失败", o.ID, requestID), "order_id", o.ID, "request_id", requestID)
		return order.Applied{}, e
	}
	obs.Source = "notify"
	obs.RequestID = requestID
	result, e := s.Orders.Apply(ctx, obs)
	if errors.Is(e, sql.ErrNoRows) {
		e = order.ErrNotFound
	}
	return result, e
}
