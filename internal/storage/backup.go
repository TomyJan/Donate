package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Backup uses SQLite's consistent snapshot operation, including committed WAL data.
func (s *Store) Backup(ctx context.Context, dir string) (string, error) {
	return s.backup(ctx, dir, nil)
}

// A restore protects its selected source only for this retention pass. The newly
// created snapshot is always retained, including when older files have future dates.
func (s *Store) backup(ctx context.Context, dir string, protected os.FileInfo) (out string, err error) {
	phase, targetPath := "create_directory", dir
	defer func() {
		if err != nil {
			err = &OperationError{Operation: "backup", Phase: phase, Path: targetPath, Err: err}
		}
	}()
	options := s.options
	if options.AppName == "" {
		options = Options{AppName: "donate", BackupRetentionDays: 7}
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	now := time.Now().UTC()
	phase = "resolve_path"
	path, e := filepath.Abs(filepath.Join(dir, fmt.Sprintf("%s-%s-%s.db", options.AppName, now.Format("20060102T150405Z"), NewID().String())))
	if e != nil {
		return "", e
	}
	phase, targetPath = "snapshot", path
	s.writeMu.Lock()
	_, e = s.DB.ExecContext(ctx, "VACUUM INTO ?", path)
	s.writeMu.Unlock()
	if e != nil {
		return "", e
	}
	phase = "permissions"
	if e = os.Chmod(path, 0600); e != nil {
		return path, e
	}
	phase = "snapshot_stat"
	created, e := os.Stat(path)
	if e != nil {
		return path, e
	}
	phase = "retention_list"
	entries, e := os.ReadDir(dir)
	if e != nil {
		return path, e
	}
	if options.BackupRetentionDays == 0 {
		return path, nil
	}
	cutoff := now.AddDate(0, 0, -options.BackupRetentionDays)
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		rest, ok := strings.CutPrefix(entry.Name(), options.AppName+"-")
		if !ok || len(rest) != 56 || rest[16] != '-' || !strings.HasSuffix(rest, ".db") {
			continue
		}
		id, err := uuid.Parse(rest[17:53])
		if err != nil || id == uuid.Nil || id.String() != rest[17:53] {
			continue
		}
		at, err := time.Parse("20060102T150405Z", rest[:16])
		if err != nil || !at.Before(cutoff) {
			continue
		}
		old := filepath.Join(dir, entry.Name())
		phase, targetPath = "retention_stat", old
		info, err := os.Stat(old)
		if err != nil {
			return path, err
		}
		if os.SameFile(info, created) || protected != nil && os.SameFile(info, protected) {
			continue
		}
		phase = "retention_remove"
		if e = os.Remove(old); e != nil {
			return path, e
		}
	}
	return path, nil
}

// Restore must run with the application's exclusive instance lock held and no open Store.
// It never migrates or writes the source snapshot. A rollback snapshot is kept before replacement.
// Its source is exempt from this restore's retention pass; later backups rotate normally.
func Restore(ctx context.Context, source, target string) error {
	return RestoreWithOptions(ctx, source, target, Options{AppName: "donate", BackupRetentionDays: 7})
}

func RestoreWithOptions(ctx context.Context, source, target string, options Options) (retErr error) {
	phase, sourcePath, targetPath := "options", source, target
	defer func() {
		if retErr != nil {
			retErr = &OperationError{Operation: "restore", Phase: phase, Source: sourcePath, Target: targetPath, Err: retErr}
		}
	}()
	var optionErr error
	options, optionErr = normalizeOptions(target, options)
	if optionErr != nil {
		return optionErr
	}
	knownVersions := map[int64]bool{}
	phase = "migration_catalog"
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
	phase = "source_path"
	src, e := filepath.Abs(source)
	if e != nil {
		return e
	}
	sourcePath, phase = src, "target_path"
	dst, e := filepath.Abs(target)
	if e != nil {
		return e
	}
	targetPath, phase = dst, "source_alias"
	if src == dst {
		return errors.New("备份路径不能与当前数据库相同")
	}
	phase = "source_stat"
	info, e := os.Stat(src)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return errors.New("备份必须是普通文件")
	}
	phase = "target_stat"
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
	phase = "source_open"
	db, e := open(src, "ro")
	if e != nil {
		return e
	}
	defer db.Close()
	phase = "source_validate"
	if e = check(db); e != nil {
		return fmt.Errorf("备份校验失败: %w", e)
	}
	phase = "target_directory"
	if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return e
	}
	stage := dst + ".restore-" + NewID().String()
	defer os.Remove(stage)
	phase = "snapshot_stage"
	if _, e = db.ExecContext(ctx, "VACUUM INTO ?", stage); e != nil {
		return e
	}
	phase = "source_close"
	if e = db.Close(); e != nil {
		return e
	}
	phase = "stage_permissions"
	if e = os.Chmod(stage, 0600); e != nil {
		return e
	}
	phase = "stage_open"
	candidate, e := open(stage, "rw")
	if e != nil {
		return e
	}
	s := &Store{DB: candidate}
	phase = "reset_transient_state"
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
		phase = "stage_validate"
		e = check(candidate)
	}
	closeErr := candidate.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		phase = "stage_close"
		return closeErr
	}
	previous := dst + ".before-restore-" + NewID().String()
	exists := false
	phase = "target_stat"
	if _, err := os.Stat(dst); err == nil {
		exists = true
		phase = "current_open"
		current, err := open(dst, "rw")
		if err != nil {
			return err
		}
		phase = "rollback_backup"
		_, err = (&Store{DB: current, options: options}).backup(ctx, options.BackupDir, info)
		if err == nil {
			phase = "current_checkpoint"
			_, err = current.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
		}
		closeErr = current.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			phase = "current_close"
			return closeErr
		}
		phase = "preserve_current"
		if e = os.Rename(dst, previous); e != nil {
			return e
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	phase = "replace_target"
	if e = os.Rename(stage, dst); e != nil {
		if exists {
			if rollback := os.Rename(previous, dst); rollback != nil {
				return errors.Join(e, fmt.Errorf("原文件位于 %s，回滚失败: %w", previous, rollback))
			}
		}
		return e
	}
	if exists {
		phase = "remove_previous"
		if e = os.Remove(previous); e != nil {
			return e
		}
	}
	return nil
}
