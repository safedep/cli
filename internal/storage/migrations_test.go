package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestStorage(t *testing.T) *sqliteImpl {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.db")
	s, err := openSqlite(context.Background(), Options{Backend: BackendSqlite, Path: p})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s.(*sqliteImpl)
}

func TestMigrations_CleanApply(t *testing.T) {
	s := newTestStorage(t)
	v, err := s.currentSchemaVersion(context.Background())
	require.NoError(t, err)
	require.Equal(t, len(loadSqliteMigrations()), v)
}

func TestMigrations_CreatesExpectedSchema(t *testing.T) {
	s := newTestStorage(t)

	for _, name := range []string{"kv", "schema_migrations"} {
		var n int
		err := s.conn.QueryRowContext(context.Background(),
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 1, n, "table %q should exist", name)
	}

	var n int
	err := s.conn.QueryRowContext(context.Background(),
		`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='kv_expires'`).Scan(&n)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestMigrations_Idempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")

	s1, err := openSqlite(context.Background(), Options{Backend: BackendSqlite, Path: p})
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	s2, err := openSqlite(context.Background(), Options{Backend: BackendSqlite, Path: p})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	v, err := s2.(*sqliteImpl).currentSchemaVersion(context.Background())
	require.NoError(t, err)
	require.Equal(t, len(loadSqliteMigrations()), v)
}

func TestMigrations_SchemaTooNew(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")

	s, err := openSqlite(context.Background(), Options{Backend: BackendSqlite, Path: p})
	require.NoError(t, err)

	impl := s.(*sqliteImpl)
	_, err = impl.conn.ExecContext(context.Background(),
		`INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`,
		9999, "from-the-future", 0)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	_, err = openSqlite(context.Background(), Options{Backend: BackendSqlite, Path: p})
	require.ErrorIs(t, err, ErrSchemaTooNew)
}

func TestMigrations_OrderingIsContiguous(t *testing.T) {
	ms := loadSqliteMigrations()
	require.NotEmpty(t, ms)
	for i, m := range ms {
		require.Equal(t, i+1, m.Version, "migration at position %d", i)
	}
}
