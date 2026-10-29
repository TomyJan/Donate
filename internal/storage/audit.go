package storage

import (
	"context"
	"database/sql"
)

func Audit(ctx context.Context, tx *sql.Tx, actor []byte, action, kind string, target []byte, result string, at int64) error {
	id := NewID()
	_, e := tx.ExecContext(ctx, "INSERT INTO audit_events(id,actor_id,action,target_type,target_id,result,created_at) VALUES(?,?,?,?,?,?,?)", id[:], actor, action, kind, target, result, at)
	return e
}
