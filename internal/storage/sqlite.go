package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// sqliteImpl is the concrete sqlite-backed Storage. The struct is
// unexported; the only entry point is Open.
type sqliteImpl struct {
	path string
	conn *sql.DB

	mu        sync.Mutex
	schemaVer int
}

func openSqlite(ctx context.Context, opts Options) (Storage, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("storage: sqlite path required")
	}
	if opts.BusyTimeout == 0 {
		opts.BusyTimeout = 5 * time.Second
	}

	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o700); err != nil {
		return nil, fmt.Errorf("storage: mkdir: %w", err)
	}

	// modernc DSN: WAL journal, foreign keys on, busy timeout.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)",
		opts.Path, opts.BusyTimeout.Milliseconds(),
	)

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open: %w", err)
	}
	// modernc.org/sqlite is single-writer at the file level. Cap
	// connections so retries hit the same writer rather than queuing
	// on a fresh handle.
	conn.SetMaxOpenConns(1)

	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}

	s := &sqliteImpl{path: opts.Path, conn: conn}
	if err := s.applyMigrations(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqliteImpl) Close() error                   { return s.conn.Close() }
func (s *sqliteImpl) db() *sql.DB                    { return s.conn }
func (s *sqliteImpl) scopeProfile(p string) string   { return "profile:" + p }
func (s *sqliteImpl) scopeGlobal() string            { return "global" }
