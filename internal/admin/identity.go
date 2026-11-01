package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

var ErrLastIdentity = errors.New("至少保留一种登录方式")

// UnbindIdentity removes a login method and its credentials in one serialized
// transaction. Old sessions must never be reassigned to the remaining identity.
func (s *Service) UnbindIdentity(ctx context.Context, actor, target, identity string) error {
	started, phase := time.Now(), "transaction_begin"
	uid, err := storage.ParseID(target)
	if err != nil {
		return auth.ErrInvalid
	}
	iid, err := storage.ParseID(identity)
	if err != nil {
		return auth.ErrInvalid
	}
	aid, _ := storage.ParseID(actor)
	err = s.Store.Write(ctx, func(tx *sql.Tx) error {
		phase = "authorization"
		if err := s.check(ctx, tx, actor); err != nil {
			return err
		}
		var exists int
		phase = "identity_read"
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM identities WHERE id=? AND user_id=?", iid, uid).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return order.ErrNotFound
		} else if err != nil {
			return err
		}
		var replacement []byte
		phase = "retained_identity_read"
		if err := tx.QueryRowContext(ctx, "SELECT id FROM identities WHERE user_id=? AND id<>? ORDER BY created_at,id LIMIT 1", uid, iid).Scan(&replacement); errors.Is(err, sql.ErrNoRows) {
			return ErrLastIdentity
		} else if err != nil {
			return err
		}
		// Cancel every outstanding binding callback for this user, including one
		// already exchanging credentials outside a transaction. Also remove flows
		// referencing the sessions about to be deleted to satisfy their foreign key.
		phase = "binding_flow_revoke"
		if _, err := tx.ExecContext(ctx, "DELETE FROM auth_flows WHERE (user_id=? AND intent='bind' AND consumed_at IS NULL) OR session_id IN (SELECT id FROM sessions WHERE identity_id=?)", uid, iid); err != nil {
			return err
		}
		// sessions.identity_id is a non-null foreign key. Physical removal both
		// invalidates the credential and permits deleting its OAuth identity.
		phase = "session_revoke"
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE identity_id=?", iid); err != nil {
			return err
		}
		phase = "public_identity_update"
		if _, err := tx.ExecContext(ctx, "UPDATE users SET public_identity_id=? WHERE id=? AND public_identity_id=?", replacement, uid, iid); err != nil {
			return err
		}
		phase = "identity_delete"
		if _, err := tx.ExecContext(ctx, "DELETE FROM identities WHERE id=? AND user_id=?", iid, uid); err != nil {
			return err
		}
		phase = "audit"
		err := storage.Audit(ctx, tx, aid, "user.identity.unbind", "user", uid, "success", s.Now().Unix())
		if err == nil {
			phase = "transaction_commit"
		}
		return err
	})
	if err == nil {
		s.Log.Info(fmt.Sprintf("管理员 %s 为用户 %s 解绑身份 %s，关联会话及未完成绑定授权已撤销", actor, target, identity), "actor_id", actor, "user_id", target, "identity_id", identity, "action", "user.identity.unbind")
	} else {
		s.logMutationFailure(phase, "user.identity.unbind", actor, map[string]string{"user_id": target, "identity_id": identity}, started, err)
	}
	return err
}
