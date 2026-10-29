package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
}

func Open(ctx context.Context, path string) (*Store, error) {
	_, statErr := os.Stat(path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	if err = db.PingContext(ctx); err != nil {
		return fail(err)
	}
	if statErr == nil {
		if _, err = (&Store{DB: db}).Backup(ctx, filepath.Join(filepath.Dir(path), "backups")); err != nil {
			return fail(fmt.Errorf("迁移前备份失败: %w", err))
		}
	}
	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fail(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrationFS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fail(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		return fail(fmt.Errorf("数据库迁移失败: %w", err))
	}
	return &Store{DB: db}, nil
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
