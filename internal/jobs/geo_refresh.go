package jobs

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

const GeoRefreshSource = "admin_refresh"

// EnqueueGeoRefreshTx commits a refresh with its admin confirmation. An active
// refresh is shared by repeated requests; an ordinary worker retains its lease
// and must finish before its forced follow-up can be claimed.
func EnqueueGeoRefreshTx(ctx context.Context, tx *sql.Tx, oid, actor []byte, requestID string, now time.Time) ([]byte, error) {
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM orders WHERE id=?", oid).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 1 {
		return nil, order.ErrNotFound
	}
	var id []byte
	var status, source string
	var lease sql.NullInt64
	err := tx.QueryRowContext(ctx, "SELECT id,status,source,lease_until FROM jobs WHERE kind='geo' AND order_id=? AND status IN ('ready','running')", oid).Scan(&id, &status, &source, &lease)
	if err == nil {
		if source == GeoRefreshSource {
			return id, nil
		}
		if status == "running" && lease.Valid && lease.Int64 > now.Unix() {
			_, err = tx.ExecContext(ctx, "UPDATE jobs SET source=?,actor_id=?,request_id=?,rerun=1,attempts=0,updated_at=? WHERE id=?", GeoRefreshSource, actor, requestID, now.Unix(), id)
		} else {
			_, err = tx.ExecContext(ctx, "UPDATE jobs SET status='ready',source=?,actor_id=?,request_id=?,run_at=?,attempts=0,rerun=0,lease_token=NULL,lease_until=NULL,updated_at=? WHERE id=?", GeoRefreshSource, actor, requestID, now.Unix(), now.Unix(), id)
		}
		return id, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	idValue := storage.NewID()
	id = idValue[:]
	_, err = tx.ExecContext(ctx, "INSERT INTO jobs(id,kind,order_id,run_at,source,actor_id,request_id,created_at,updated_at) VALUES(?,'geo',?,?,?,?,?,?,?)", id, oid, now.Unix(), GeoRefreshSource, actor, requestID, now.Unix(), now.Unix())
	return id, err
}
