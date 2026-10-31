//go:build e2e

package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/TomyJan/Donate/internal/storage"
)

// seedTaskQueue runs before the app starts, against the newly created temporary
// E2E database only. All history is produced by the production queue triggers.
func seedTaskQueue(store *storage.Store) error {
	rows, err := store.DB.Query("SELECT id FROM orders ORDER BY created_at,id")
	if err != nil {
		return err
	}
	var ids [][]byte
	for rows.Next() {
		var id []byte
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	future := time.Now().Add(24 * time.Hour).Unix()
	return store.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM jobs"); err != nil {
			return err
		}
		for i, oid := range ids {
			for k, kind := range []string{"payment", "geo", "close"} {
				id := storage.NewID()
				status := []string{"failed", "ready", "running", "done"}[(i*3+k)%4]
				source := []string{"automatic", "manual", "admin_refresh", "close_followup"}[(i+k)%4]
				if _, err := tx.Exec("INSERT INTO jobs(id,kind,order_id,status,run_at,source,request_id,created_at,updated_at) VALUES(?,?,?,'ready',?,?,?,?,?)", id[:], kind, oid, future, source, fmt.Sprintf("fixture-task-%d-%d", i, k), now-int64(i*60+k), now); err != nil {
					return err
				}
				if i == 0 && k == 0 {
					for attempt := range 12 {
						if _, err := tx.Exec("UPDATE jobs SET status='running',attempts=attempts+1,lease_token=?,lease_until=?,updated_at=? WHERE id=?", fmt.Sprintf("fixture-lease-%d", attempt), future, now, id[:]); err != nil {
							return err
						}
						if _, err := tx.Exec("UPDATE jobs SET status='ready',lease_token=NULL,last_result='query_failed',updated_at=? WHERE id=?", now, id[:]); err != nil {
							return err
						}
					}
				}
				if _, err := tx.Exec("UPDATE jobs SET status=?,lease_token=CASE WHEN ?='running' THEN 'fixture-active-lease' ELSE NULL END,lease_until=?,last_result=CASE WHEN ?='failed' THEN 'query_failed' WHEN ?='done' THEN 'fixture_unknown_result' ELSE '' END,updated_at=? WHERE id=?", status, status, future, status, status, now, id[:]); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
