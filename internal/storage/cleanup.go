package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/safedep/dry/log"
)

// Verbs used in cleanup log lines. Distinguishing dry-run output from a
// real run matters for operators reading logs.
const (
	cleanupVerbDeleted     = "deleted"
	cleanupVerbWouldDelete = "would delete"
)

// Cleanup applies retention to every registered primitive. `now` and
// `now - retention` are Go-computed unix nanos bound as parameters;
// the DB never computes time.
func (s *sqliteImpl) Cleanup(ctx context.Context, policy CleanupPolicy) (CleanupReport, error) {
	now := time.Now().UnixNano()

	var sizeBefore int64
	if info, err := os.Stat(s.path); err == nil {
		sizeBefore = info.Size()
	}

	report := CleanupReport{}
	for _, p := range primitives {
		retention := p.DefaultRetention
		if v, ok := policy.MaxAge[p.Name]; ok {
			retention = v
		}

		deleted, err := s.cleanupDescriptor(ctx, p, retention, now, policy.DryRun)
		if err != nil {
			return CleanupReport{}, fmt.Errorf("storage: cleanup %s: %w", p.Name, err)
		}
		report.Primitives = append(report.Primitives, PrimitiveCleanup{
			Name:          p.Name,
			DeletedRows:   deleted,
			PolicySeconds: int64(retention.Seconds()),
		})

		verb := cleanupVerbDeleted
		if policy.DryRun {
			verb = cleanupVerbWouldDelete
		}
		log.Infof("storage: cleanup %s: %s %d rows (retention %s)", p.Name, verb, deleted, retention)
	}

	if policy.Vacuum && !policy.DryRun {
		if _, err := s.conn.ExecContext(ctx, `VACUUM`); err != nil {
			return CleanupReport{}, fmt.Errorf("storage: vacuum: %w", err)
		}
		report.Vacuumed = true
		if info, err := os.Stat(s.path); err == nil {
			d := sizeBefore - info.Size()
			if d > 0 {
				report.Reclaimed = d
			}
		}
	}
	return report, nil
}

func (s *sqliteImpl) cleanupDescriptor(
	ctx context.Context,
	d primitiveDescriptor,
	retention time.Duration,
	now int64,
	dryRun bool,
) (int64, error) {
	pred, args := buildCleanupPredicate(d, retention, now)
	if pred == "" {
		return 0, nil
	}

	// Identifiers come from primitiveDescriptor (compile-time constants);
	// values are bound. The predicate is composed only from descriptor
	// columns inside this package.
	if dryRun {
		//nolint:gosec // G201: identifier interpolation from trusted constants.
		q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s`, d.Table, pred)
		var n int64
		if err := s.conn.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			return 0, err
		}
		return n, nil
	}

	//nolint:gosec // G201: identifier interpolation from trusted constants.
	q := fmt.Sprintf(`DELETE FROM %s WHERE %s`, d.Table, pred)
	res, err := s.conn.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return rowsAffected(res), nil
}

// buildCleanupPredicate composes the WHERE clause for a descriptor.
// Returns ("", nil) when the descriptor is not eligible for any
// deletion (no TTL column AND no retention).
func buildCleanupPredicate(d primitiveDescriptor, retention time.Duration, now int64) (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if d.ExpiresAtCol != "" {
		clauses = append(clauses, fmt.Sprintf("(%s IS NOT NULL AND %s <= ?)", d.ExpiresAtCol, d.ExpiresAtCol))
		args = append(args, now)
	}
	if retention > 0 {
		clauses = append(clauses, fmt.Sprintf("%s <= ?", d.CreatedAtCol))
		args = append(args, now-retention.Nanoseconds())
	}
	if len(clauses) == 0 {
		return "", nil
	}
	pred := clauses[0]
	for _, c := range clauses[1:] {
		pred += " OR " + c
	}
	return pred, args
}

func rowsAffected(res sql.Result) int64 {
	n, err := res.RowsAffected()
	if err != nil {
		return 0
	}
	return n
}
