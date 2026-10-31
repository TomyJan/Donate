package order

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/TomyJan/Donate/internal/storage"
	"time"
)

type Observation struct {
	LeaseToken                                                                                            string
	MerchantOrderNo, MerchantID, GatewayID, TradeNo, APITradeNo, Channel, State, Source, RequestID, JobID string
	AmountFen                                                                                             int64
	PaidAt                                                                                                time.Time
	Verified                                                                                              bool
}
type Applied struct {
	OrderID string `json:"order_id"`
	State   string `json:"state"`
	Changed bool   `json:"changed"`
}

func (s *Service) Apply(ctx context.Context, o Observation) (Applied, error) {
	var result Applied
	if !o.Verified {
		return result, ErrMismatch
	}
	now := s.Now()
	late := false
	e := s.Store.Write(ctx, func(tx *sql.Tx) error {
		if o.JobID != "" {
			jid, err := storage.ParseID(o.JobID)
			if err != nil {
				return ErrBusy
			}
			var count int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs j JOIN orders o ON o.id=j.order_id WHERE j.id=? AND j.kind IN ('payment','close') AND j.status='running' AND j.lease_token=? AND j.lease_until>? AND o.merchant_order_no=?", jid, o.LeaseToken, now.Unix(), o.MerchantOrderNo).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return ErrBusy
			}
		}
		v, e := Scan(tx.QueryRowContext(ctx, "SELECT "+Columns+" FROM orders WHERE merchant_order_no=?", o.MerchantOrderNo))
		if e != nil {
			return e
		}
		id, _ := storage.ParseID(v.ID)
		result.OrderID = v.ID
		result.State = v.Status
		if v.AmountFen != o.AmountFen || v.MerchantID != o.MerchantID || v.GatewayID != o.GatewayID || v.Channel != o.Channel || v.TradeNo != "" && o.TradeNo != "" && v.TradeNo != o.TradeNo {
			return ErrMismatch
		}
		if o.State == "paid" && o.TradeNo == "" {
			return ErrMismatch
		}
		if o.State == "paid" && v.Status != "paid" {
			at := o.PaidAt
			if at.IsZero() {
				at = now
			}
			if at.After(now.Add(5 * time.Minute)) {
				return ErrMismatch
			}
			_, e = tx.ExecContext(ctx, "UPDATE orders SET status='paid',paid_at=?,trade_no=?,api_trade_no=?,gateway_status='paid',last_checked_at=?,version=version+1 WHERE id=?", at.Unix(), o.TradeNo, o.APITradeNo, now.Unix(), id)
			if storage.Conflict(e) {
				return ErrMismatch
			}
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "UPDATE jobs SET status='done',lease_token=NULL,lease_until=NULL,last_result='paid',event_source=?,event_request_id=?,updated_at=? WHERE order_id=? AND kind IN ('payment','close') AND status IN ('ready','running')", o.Source, o.RequestID, now.Unix(), id); e != nil {
				return e
			}
			if e = storage.Audit(ctx, tx, nil, "order.paid", "order", id, "paid", now.Unix()); e != nil {
				return e
			}
			result.State = "paid"
			result.Changed = true
			late = v.Status == "closed"
		} else {
			state := o.State
			if _, e = tx.ExecContext(ctx, "UPDATE orders SET gateway_status=?,last_checked_at=? WHERE id=?", state, now.Unix(), id); e != nil {
				return e
			}
		}
		if o.State == "paid" && !result.Changed {
			return nil
		}
		observation := storage.NewID()
		var job any
		if o.JobID != "" {
			job, _ = storage.ParseID(o.JobID)
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO payment_observations(id,order_id,source,request_id,job_id,trade_no,status,amount_fen,observed_at,result) VALUES(?,?,?,?,?,?,?,?,?,?)", observation[:], id, o.Source, o.RequestID, job, o.TradeNo, o.State, o.AmountFen, now.Unix(), result.State)
		return e
	})
	if e != nil {
		if errors.Is(e, ErrMismatch) {
			s.Log.Error(fmt.Sprintf("商户订单 %s 的请求 %s 支付校验不匹配，拒绝入账，渠道金额 %d 分", o.MerchantOrderNo, o.RequestID, o.AmountFen), "merchant_order_no", o.MerchantOrderNo, "request_id", o.RequestID, "amount_fen", o.AmountFen)
		}
		return result, e
	}
	if result.Changed {
		s.Log.Info(fmt.Sprintf("订单 %s 已由 %s 请求 %s 确认为已支付，渠道流水 %s，金额 %d 分", result.OrderID, o.Source, o.RequestID, o.TradeNo, o.AmountFen), "order_id", result.OrderID, "request_id", o.RequestID, "trade_no", o.TradeNo, "amount_fen", o.AmountFen)
		if late {
			s.Log.Warn(fmt.Sprintf("订单 %s 在本站关闭后确认到账，已按实际结果入账", result.OrderID), "order_id", result.OrderID)
		}
	}
	if result.State == "paid" && o.State != "paid" {
		s.Log.Warn(fmt.Sprintf("订单 %s 已入账但渠道请求 %s 返回状态 %s，保留到账依据并记录异常", result.OrderID, o.RequestID, o.State), "order_id", result.OrderID, "request_id", o.RequestID, "gateway_status", o.State)
	}
	return result, nil
}
