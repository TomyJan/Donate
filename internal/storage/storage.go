package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	DB      *sql.DB
	writeMu sync.Mutex
	options Options
}

type Options struct {
	AppName, BackupDir  string
	BackupRetentionDays int
}

func Open(ctx context.Context, path string) (*Store, error) {
	return OpenWithOptions(ctx, path, Options{AppName: "donate", BackupRetentionDays: 7})
}

func OpenWithOptions(ctx context.Context, path string, options Options) (*Store, error) {
	var err error
	options, err = normalizeOptions(path, options)
	if err != nil {
		return nil, &OperationError{Operation: "open", Phase: "options", Path: path, Err: err}
	}
	requestedPath := path
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, &OperationError{Operation: "open", Phase: "resolve_path", Path: requestedPath, Err: err}
	}
	return openWithOptions(ctx, path, options)
}

func normalizeOptions(path string, options Options) (Options, error) {
	if options.AppName == "" {
		options.AppName = "donate"
	}
	if options.BackupRetentionDays < 0 || len(options.AppName) > 63 || !regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`).MatchString(options.AppName) {
		return options, fmt.Errorf("备份名称或保留天数无效")
	}
	if options.BackupDir == "" {
		options.BackupDir = filepath.Join(filepath.Dir(path), "backups")
	}
	return options, nil
}

func openWithOptions(ctx context.Context, path string, options Options) (out *Store, retErr error) {
	phase := "stat"
	defer func() {
		if retErr != nil {
			retErr = &OperationError{Operation: "open", Phase: phase, Path: path, Err: retErr}
		}
	}()
	_, statErr := os.Stat(path)
	if os.IsNotExist(statErr) {
		phase = "restore_recovery_check"
		// Restore replaces the live file with two renames. A crash between them
		// must not turn a recoverable database into a new empty installation.
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		base := filepath.Base(path)
		if runtime.GOOS == "windows" {
			base = strings.ToLower(base)
		}
		for _, entry := range entries {
			name := entry.Name()
			if runtime.GOOS == "windows" {
				name = strings.ToLower(name)
			}
			if strings.HasPrefix(name, base+".before-restore-") || strings.HasPrefix(name, base+".restore-") {
				return nil, fmt.Errorf("当前数据库 %q 缺失，检测到中断的恢复文件 %q；请先检查并恢复原库或暂存库", path, entry.Name())
			}
		}
	} else if statErr != nil {
		return nil, statErr
	}
	phase = "create_directory"
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	phase = "file_open"
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	phase = "file_permissions"
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	phase = "file_close"
	if err = file.Close(); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		phase = "journal_permissions"
		if err = os.Chmod(path+suffix, 0600); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	u := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}
	q := u.Query()
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)"} {
		q.Add("_pragma", pragma)
	}
	u.RawQuery = q.Encode()
	phase = "connect"
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	phase = "ping"
	if err = db.PingContext(ctx); err != nil {
		return fail(err)
	}
	if statErr == nil {
		phase = "migration_backup"
		if _, err = (&Store{DB: db, options: options}).Backup(ctx, options.BackupDir); err != nil {
			return fail(fmt.Errorf("迁移前备份失败: %w", err))
		}
	}
	phase = "migration_catalog"
	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fail(err)
	}
	phase = "migration_provider"
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fail(err)
	}

	// Table rebuilds must not cascade-delete dependent rows. Startup has one
	// connection and no serving requests; each Goose migration remains atomic.
	phase = "foreign_keys_suspend"
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return fail(err)
	}
	_, migrationErr := provider.Up(ctx)
	phase = "foreign_keys_enable"
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return fail(err)
	}
	if migrationErr != nil {
		phase = "migration_apply"
		return fail(fmt.Errorf("数据库迁移失败: %w", migrationErr))
	}
	phase = "foreign_keys_validate"
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fail(err)
	}
	invalid := rows.Next()
	checkErr := rows.Err()
	rows.Close()
	if checkErr != nil {
		return fail(checkErr)
	}
	if invalid {
		return fail(fmt.Errorf("数据库迁移后外键校验失败"))
	}

	return &Store{DB: db, options: options}, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
func NewID() uuid.UUID            { return uuid.Must(uuid.NewV7()) }
func IDBytes(id uuid.UUID) []byte { return id[:] }
func ParseID(v string) ([]byte, error) {
	id, e := uuid.Parse(v)
	if e != nil {
		return nil, e
	}
	return id[:], nil
}
func IDString(v []byte) string {
	id, e := uuid.FromBytes(v)
	if e != nil {
		return ""
	}
	return id.String()
}
func Conflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "constraint failed")
}
