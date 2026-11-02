package admin

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

var ErrOrderMigrationNotAllowed = errors.New("只能迁移尚未关联用户的历史订单")

type MigrationResult struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Result string `json:"result"`
}

// MigrateLegacyOrder attaches historical evidence to an existing account. It
// deliberately updates only ownership and version: donor/payment/geo snapshots
// remain evidence of the original donation, regardless of the account profile.
func (s *Service) MigrateLegacyOrder(ctx context.Context, actor, id, userID string) (MigrationResult, error) {
	started, phase := time.Now(), "transaction_begin"
	oid, err := storage.ParseID(id)
	if err != nil {
		return MigrationResult{}, auth.ErrInvalid
	}
	uid, err := storage.ParseID(userID)
	if err != nil {
		return MigrationResult{}, auth.ErrInvalid
	}
	aid, _ := storage.ParseID(actor)
	out := MigrationResult{ID: storage.IDString(oid), UserID: storage.IDString(uid)}
	err = s.Store.Write(ctx, func(tx *sql.Tx) error {
		phase = "authorization"
		if err := s.check(ctx, tx, actor); errors.Is(err, sql.ErrNoRows) {
			return auth.ErrUnauthorized
		} else if err != nil {
			return err
		}
		phase = "order_read"
		var owner []byte
		var protocol string
		if err := tx.QueryRowContext(ctx, "SELECT user_id,protocol FROM orders WHERE id=?", oid).Scan(&owner, &protocol); errors.Is(err, sql.ErrNoRows) {
			return order.ErrNotFound
		} else if err != nil {
			return err
		}
		phase = "user_read"
		var exists int
		// Banned accounts still own their history; ownership grants no access.
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM users WHERE id=?", uid).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return order.ErrNotFound
		} else if err != nil {
			return err
		}
		phase = "migration_guard"
		if protocol != "legacy" {
			return ErrOrderMigrationNotAllowed
		}
		if bytes.Equal(owner, uid) {
			out.Result = "unchanged"
			return nil
		}
		if owner != nil {
			return ErrOrderMigrationNotAllowed
		}
		phase = "order_update"
		r, err := tx.ExecContext(ctx, "UPDATE orders SET user_id=?,version=version+1 WHERE id=? AND protocol='legacy' AND user_id IS NULL", uid, oid)
		if err != nil {
			return err
		}
		changed, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrOrderMigrationNotAllowed
		}
		phase = "audit"
		if err := storage.Audit(ctx, tx, aid, "order.migrate", "order", oid, "migrated_to:"+out.UserID, s.Now().Unix()); err != nil {
			return err
		}
		out.Result = "migrated"
		phase = "transaction_commit"
		return nil
	})
	if err != nil {
		s.logMutationFailure(phase, "order.migrate", storage.IDString(aid), map[string]string{"order_id": out.ID, "user_id": out.UserID}, started, err)
		return MigrationResult{}, err
	}
	s.Log.Info(fmt.Sprintf("管理员 %s 将历史订单 %s 关联到用户 %s，结果 %s", actor, out.ID, out.UserID, out.Result), "actor_id", actor, "order_id", out.ID, "user_id", out.UserID, "action", "order.migrate", "result", out.Result)
	return out, nil
}
