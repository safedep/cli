package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

// Stats reports per-primitive usage. Walks the descriptor table issuing
// canned SQL per primitive; no primitive-specific knowledge lives in
// the walker.
func (s *sqliteImpl) Stats(ctx context.Context) (DBStats, error) {
	now := time.Now().UnixNano()

	out := DBStats{Path: s.path}

	if info, err := os.Stat(s.path); err == nil {
		out.SizeBytes = info.Size()
	}

	ver, err := s.currentSchemaVersion(ctx)
	if err != nil {
		return DBStats{}, err
	}
	out.SchemaVer = ver

	for _, p := range primitives {
		ps, err := s.statsForDescriptor(ctx, p, now)
		if err != nil {
			return DBStats{}, fmt.Errorf("storage: stats %s: %w", p.Name, err)
		}
		out.Primitives = append(out.Primitives, ps)
	}
	return out, nil
}

func (s *sqliteImpl) statsForDescriptor(ctx context.Context, d primitiveDescriptor, now int64) (PrimitiveStats, error) {
	ps := PrimitiveStats{Name: d.Name}

	// Identifiers come from primitiveDescriptor (compile-time constants),
	// never user input. Bind parameters are still used for values.
	//nolint:gosec // G201: identifier interpolation from trusted constants.
	q := fmt.Sprintf(
		`SELECT count(*), min(%s), max(%s) FROM %s`,
		d.CreatedAtCol, d.CreatedAtCol, d.Table,
	)
	var (
		count int64
		oMin  sql.NullInt64
		oMax  sql.NullInt64
	)
	if err := s.conn.QueryRowContext(ctx, q).Scan(&count, &oMin, &oMax); err != nil {
		return PrimitiveStats{}, err
	}
	ps.RowCount = count
	if oMin.Valid {
		t := time.Unix(0, oMin.Int64)
		ps.OldestEntry = &t
	}
	if oMax.Valid {
		t := time.Unix(0, oMax.Int64)
		ps.NewestEntry = &t
	}

	if d.ExpiresAtCol != "" {
		//nolint:gosec // G201: identifier interpolation from trusted constants.
		eq := fmt.Sprintf(
			`SELECT count(*) FROM %s WHERE %s IS NOT NULL AND %s <= ?`,
			d.Table, d.ExpiresAtCol, d.ExpiresAtCol,
		)
		var expired int64
		if err := s.conn.QueryRowContext(ctx, eq, now).Scan(&expired); err != nil {
			return PrimitiveStats{}, err
		}
		ps.ExpiredRows = expired
	}
	return ps, nil
}
