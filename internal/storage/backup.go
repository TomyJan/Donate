package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Backup uses SQLite's consistent snapshot operation, including committed WAL data.
func (s *Store) Backup(ctx context.Context, dir string) (string, error) {
	return s.backup(ctx, dir, nil)
}

// A restore protects its selected source only for this retention pass. The newly
// created snapshot is always retained, including when older files have future dates.
func (s *Store) backup(ctx context.Context, dir string, protected os.FileInfo) (string, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	path, e := filepath.Abs(filepath.Join(dir, fmt.Sprintf("donate-%s-%s.db", time.Now().UTC().Format("20060102T150405Z"), NewID().String())))
	if e != nil {
		return "", e
	}
	s.writeMu.Lock()
	_, e = s.DB.ExecContext(ctx, "VACUUM INTO ?", path)
	s.writeMu.Unlock()
	if e != nil {
		return "", e
	}
	if e = os.Chmod(path, 0600); e != nil {
		return path, e
	}
	created, e := os.Stat(path)
	if e != nil {
		return path, e
	}
	paths, e := filepath.Glob(filepath.Join(dir, "donate-*.db"))
	if e != nil {
		return path, e
	}
	sort.Strings(paths)
	remaining := len(paths) - 7
	for _, old := range paths {
		if remaining <= 0 {
			break
		}
		info, err := os.Stat(old)
		if err != nil {
			return path, err
		}
		if os.SameFile(info, created) || protected != nil && os.SameFile(info, protected) {
			continue
		}
		if e = os.Remove(old); e != nil {
			return path, e
		}
		remaining--
	}
	return path, nil
}

// Restore must run with the application's exclusive instance lock held and no open Store.
// It never migrates or writes the source snapshot. A rollback snapshot is kept before replacement.
// Its source is exempt from this restore's retention pass; later backups rotate normally.
func Restore(ctx context.Context, source, target string) error {
	knownVersions := map[int64]bool{}
	entries, e := migrations.ReadDir("migrations")
	if e != nil {
		return e
	}
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			return fmt.Errorf("迁移文件名无效: %s", entry.Name())
		}
		knownVersions[version] = true
	}
	src, e := filepath.Abs(source)
	if e != nil {
		return e
	}
	dst, e := filepath.Abs(target)
	if e != nil {
		return e
	}
	if src == dst {
		return errors.New("备份路径不能与当前数据库相同")
	}
	info, e := os.Stat(src)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return errors.New("备份必须是普通文件")
	}
	if targetInfo, err := os.Stat(dst); err == nil {
		if os.SameFile(info, targetInfo) {
			return errors.New("备份路径不能与当前数据库相同")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	open := func(path, mode string) (*sql.DB, error) {
		u := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}
		q := u.Query()
		q.Set("mode", mode)
		q.Set("_pragma", "foreign_keys(1)")
		u.RawQuery = q.Encode()
		db, e := sql.Open("sqlite", u.String())
		if e != nil {
			return nil, e
		}
		db.SetMaxOpenConns(1)
		if e = db.PingContext(ctx); e != nil {
			db.Close()
			return nil, e
		}
		return db, nil
	}
	check := func(db *sql.DB) error {
		var result string
		if e := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); e != nil {
			return e
		}
		if result != "ok" {
			return errors.New("备份完整性校验失败")
		}
		rows, e := db.QueryContext(ctx, "PRAGMA foreign_key_check")
		if e != nil {
			return e
		}
		has := rows.Next()
		err := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if has {
			return errors.New("备份外键校验失败")
		}
		var version int64
		if e = db.QueryRowContext(ctx, "SELECT version_id FROM goose_db_version WHERE is_applied=1 ORDER BY id DESC LIMIT 1").Scan(&version); e != nil {
			return e
		}
		// Older shipped snapshots are migrated on the next Open; unknown versions must never be downgraded.
		if !knownVersions[version] {
			return fmt.Errorf("备份迁移版本 %d 与本程序不兼容", version)
		}
		return nil
	}
	db, e := open(src, "ro")
	if e != nil {
		return e
	}
	defer db.Close()
	if e = check(db); e != nil {
		return fmt.Errorf("备份校验失败: %w", e)
	}
	if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return e
	}
	stage := dst + ".restore-" + NewID().String()
	defer os.Remove(stage)
	if _, e = db.ExecContext(ctx, "VACUUM INTO ?", stage); e != nil {
		return e
	}
	if e = db.Close(); e != nil {
		return e
	}
	if e = os.Chmod(stage, 0600); e != nil {
		return e
	}
	candidate, e := open(stage, "rw")
	if e != nil {
		return e
	}
	s := &Store{DB: candidate}
	e = s.Write(ctx, func(tx *sql.Tx) error {
		for _, query := range []string{"DELETE FROM auth_flows", "DELETE FROM sessions", "UPDATE orders SET submission_status=CASE WHEN submission_status='submitting' THEN 'unknown' ELSE submission_status END,submission_lease=NULL,submission_lease_until=NULL"} {
			if _, err := tx.ExecContext(ctx, query); err != nil {
				return err
			}
		}
		// Older snapshots are migrated only after Restore. Reset the common
		// lease fields on every supported schema; add telemetry only when the
		// snapshot already has task-history columns. Downtime is not execution
		// time, so the restored worker's start is explicitly unknown.
		var historyColumns int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('jobs') WHERE name IN ('started_at','event_source','event_request_id')").Scan(&historyColumns); err != nil {
			return err
		}
		query := "UPDATE jobs SET status='ready',lease_token=NULL,lease_until=NULL,updated_at=?"
		if historyColumns == 3 {
			query += ",started_at=NULL,event_source='restore',event_request_id=''"
		}
		_, err := tx.ExecContext(ctx, query+" WHERE status='running'", time.Now().Unix())
		return err
	})
	if e == nil {
		e = check(candidate)
	}
	closeErr := candidate.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	previous := dst + ".before-restore-" + NewID().String()
	exists := false
	if _, err := os.Stat(dst); err == nil {
		exists = true
		current, err := open(dst, "rw")
		if err != nil {
			return err
		}
		_, err = (&Store{DB: current}).backup(ctx, filepath.Join(filepath.Dir(dst), "backups"), info)
		if err == nil {
			_, err = current.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
		}
		closeErr = current.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if e = os.Rename(dst, previous); e != nil {
			return e
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if e = os.Rename(stage, dst); e != nil {
		if exists {
			if rollback := os.Rename(previous, dst); rollback != nil {
				return errors.Join(e, fmt.Errorf("原文件位于 %s，回滚失败: %w", previous, rollback))
			}
		}
		return e
	}
	if exists {
		if e = os.Remove(previous); e != nil {
			return e
		}
	}
	return nil
}
