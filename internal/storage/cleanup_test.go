package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCleanup_DeletesExpired(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "cln-ttl")
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, kv.Put(ctx, "live", "v"))
	require.NoError(t, kv.PutWithTTL(ctx, "exp", "v", 5*time.Millisecond))
	time.Sleep(20 * time.Millisecond)

	report, err := s.Cleanup(ctx, CleanupPolicy{})
	require.NoError(t, err)
	require.Len(t, report.Primitives, 1)
	require.Equal(t, int64(1), report.Primitives[0].DeletedRows)

	// Live row remains; expired is gone.
	has, err := kv.Has(ctx, "live")
	require.NoError(t, err)
	require.True(t, has)
}

func TestCleanup_RetentionDeletesOlderThan(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "cln-ret")
	require.NoError(t, err)
	ctx := context.Background()

	// Insert a row, then backdate created_at to 1 hour ago.
	require.NoError(t, kv.Put(ctx, "old", "v"))
	require.NoError(t, kv.Put(ctx, "new", "v"))

	old := time.Now().Add(-time.Hour).UnixNano()
	_, err = s.db().ExecContext(ctx,
		`UPDATE kv SET created_at = ? WHERE namespace = 'cln-ret' AND key = 'old'`, old)
	require.NoError(t, err)

	report, err := s.Cleanup(ctx, CleanupPolicy{
		MaxAge: map[PrimitiveName]time.Duration{PrimitiveKV: 30 * time.Minute},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), report.Primitives[0].DeletedRows)
	require.Equal(t, int64(30*60), report.Primitives[0].PolicySeconds)

	has, err := kv.Has(ctx, "new")
	require.NoError(t, err)
	require.True(t, has)
	has, err = kv.Has(ctx, "old")
	require.NoError(t, err)
	require.False(t, has)
}

func TestCleanup_DryRunDoesNotDelete(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "cln-dry")
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, kv.PutWithTTL(ctx, "exp", "v", 5*time.Millisecond))
	time.Sleep(20 * time.Millisecond)

	report, err := s.Cleanup(ctx, CleanupPolicy{DryRun: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), report.Primitives[0].DeletedRows)

	// Row still present (raw SQL bypasses the TTL filter applied by KV).
	var n int
	require.NoError(t, s.db().QueryRowContext(ctx,
		`SELECT count(*) FROM kv WHERE namespace = 'cln-dry'`).Scan(&n))
	require.Equal(t, 1, n)
}

func TestCleanup_VacuumNeverIncreasesSize(t *testing.T) {
	s := newTestStorage(t)
	impl := s
	kv, err := NewGlobalKV[string](impl, "cln-vac")
	require.NoError(t, err)
	ctx := context.Background()

	// Pre-populate with enough payload to make page reclamation likely
	// while keeping the assertion safe even on tiny DBs.
	for i := 0; i < 200; i++ {
		require.NoError(t, kv.PutWithTTL(ctx, fmt.Sprintf("k-%d", i),
			"a moderately-sized value to fill a page or two", 5*time.Millisecond))
	}
	time.Sleep(20 * time.Millisecond)

	report, err := impl.Cleanup(ctx, CleanupPolicy{Vacuum: true})
	require.NoError(t, err)
	require.True(t, report.Vacuumed)
	require.True(t, report.Reclaimed >= 0)
}

func TestCleanup_NoRetentionMeansTTLOnly(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "cln-zero")
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, kv.Put(ctx, "no-ttl", "v"))
	require.NoError(t, kv.PutWithTTL(ctx, "expired", "v", 5*time.Millisecond))
	time.Sleep(20 * time.Millisecond)

	// Explicit zero retention. Should still delete TTL'd row, leave the
	// no-TTL row alone.
	report, err := s.Cleanup(ctx, CleanupPolicy{
		MaxAge: map[PrimitiveName]time.Duration{PrimitiveKV: 0},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), report.Primitives[0].DeletedRows)

	has, err := kv.Has(ctx, "no-ttl")
	require.NoError(t, err)
	require.True(t, has)
}
