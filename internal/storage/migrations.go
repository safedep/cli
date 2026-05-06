package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/safedep/dry/log"
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrationsFS embed.FS

type migration struct {
	Version int
	Name    string
	SQL     string
}

// loadSqliteMigrations returns the embedded migrations sorted by
// version. Panics on a malformed embed: the data is build-time
// constant, so a bad name is a developer error caught at process
// start, not a runtime fault.
func loadSqliteMigrations() []migration {
	const dir = "migrations/sqlite"
	entries, err := fs.ReadDir(sqliteMigrationsFS, dir)
	if err != nil {
		panic(fmt.Sprintf("storage: read embedded migrations: %v", err))
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		name := e.Name()
		under := strings.IndexByte(name, '_')
		if under <= 0 {
			panic(fmt.Sprintf("storage: migration %q does not match NNNN_<name>.sql", name))
		}
		ver, err := strconv.Atoi(name[:under])
		if err != nil {
			panic(fmt.Sprintf("storage: migration %q version: %v", name, err))
		}
		body, err := fs.ReadFile(sqliteMigrationsFS, path.Join(dir, name))
		if err != nil {
			panic(fmt.Sprintf("storage: read %q: %v", name, err))
		}
		out = append(out, migration{
			Version: ver,
			Name:    strings.TrimSuffix(name[under+1:], ".sql"),
			SQL:     string(body),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	// Strictly increasing, contiguous, starting at 1. Catches renumbered
	// or missing files at process start.
	for i, m := range out {
		want := i + 1
		if m.Version != want {
			panic(fmt.Sprintf("storage: migration order: position %d has version %d, want %d", i, m.Version, want))
		}
	}
	return out
}

// applyMigrations advances the DB to the latest known schema version.
// Each migration runs in its own transaction; a failure rolls back the
// partial schema.
func (s *sqliteImpl) applyMigrations(ctx context.Context) error {
	migrations := loadSqliteMigrations()

	// Bootstrap so the SELECT below works on a brand-new DB. The 0001
	// migration also creates this table (IF NOT EXISTS), which is a
	// no-op the second time.
	if _, err := s.conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at INTEGER NOT NULL
		) STRICT
	`); err != nil {
		return fmt.Errorf("storage: bootstrap schema_migrations: %w", err)
	}

	current, err := s.currentSchemaVersion(ctx)
	if err != nil {
		return err
	}

	if current > len(migrations) {
		return ErrSchemaTooNew
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if err := s.applyOne(ctx, m); err != nil {
			return fmt.Errorf("storage: apply migration %d (%s): %w", m.Version, m.Name, err)
		}
		log.Infof("storage: applied migration %d %s", m.Version, m.Name)
	}

	s.mu.Lock()
	s.schemaVer = len(migrations)
	s.mu.Unlock()
	return nil
}

func (s *sqliteImpl) currentSchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := s.conn.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("storage: read schema version: %w", err)
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func (s *sqliteImpl) applyOne(ctx context.Context, m migration) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`,
		m.Version, m.Name, time.Now().UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}
