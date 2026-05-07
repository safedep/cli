// Package storage owns the CLI's persistent-state layer. It is the
// only package allowed to import a sql driver or write raw SQL.
// Commands access primitives (KV[T], etc.) through accessors on
// internal/app; they MUST NOT construct primitives directly or reach
// into this package's unexported surface.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Backend identifies the concrete storage implementation. Only
// BackendSqlite is implemented today; the constants are reserved for
// the daemon-mode swap path described in the ADR.
const (
	BackendSqlite   = "sqlite"
	BackendPostgres = "postgres"
	BackendMySQL    = "mysql"
)

// Options configures Open. Backend selects the implementation; Path is
// the absolute file path for sqlite (ignored for other backends).
type Options struct {
	Backend     string
	Path        string
	BusyTimeout time.Duration
}

// Storage is the dialect-neutral contract every backend implementation
// satisfies. Primitives live in this package; commands talk to them
// (not to Storage) for normal data access. Storage exposes only the
// cross-cutting operations doctor and cleanup need.
type Storage interface {
	Stats(ctx context.Context) (DBStats, error)
	Cleanup(ctx context.Context, policy CleanupPolicy) (CleanupReport, error)
	Close() error

	// db returns the underlying *sql.DB for primitives in this package.
	// Unexported so callers outside internal/storage cannot reach raw
	// SQL.
	db() *sql.DB

	// scopeProfile renders the scope string for a profile-scoped
	// primitive: "profile:<name>".
	scopeProfile(profile string) string

	// scopeGlobal returns the global scope literal: "global".
	scopeGlobal() string
}

// DBStats summarises overall database usage for the doctor command.
type DBStats struct {
	Path       string
	SizeBytes  int64
	SchemaVer  int
	Primitives []PrimitiveStats
}

// PrimitiveStats reports per-primitive usage.
type PrimitiveStats struct {
	Name        PrimitiveName
	RowCount    int64
	OldestEntry *time.Time
	NewestEntry *time.Time
	ExpiredRows int64
}

// CleanupPolicy controls the cleanup walk.
type CleanupPolicy struct {
	DryRun bool
	MaxAge map[PrimitiveName]time.Duration
	Vacuum bool
}

// CleanupReport summarises the result of a Cleanup invocation.
type CleanupReport struct {
	Primitives []PrimitiveCleanup
	Vacuumed   bool
	Reclaimed  int64
}

// PrimitiveCleanup is the per-primitive line item in CleanupReport.
type PrimitiveCleanup struct {
	Name          PrimitiveName
	DeletedRows   int64
	PolicySeconds int64
}

// Open returns a Storage backed by Options.Backend. Today only
// BackendSqlite is implemented.
func Open(ctx context.Context, opts Options) (Storage, error) {
	switch opts.Backend {
	case "", BackendSqlite:
		return openSqlite(ctx, opts)
	default:
		return nil, fmt.Errorf("storage: backend %q not supported", opts.Backend)
	}
}
