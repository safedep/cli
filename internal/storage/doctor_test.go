package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStats_ReportsRowCountsAndExpiry(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	kv, err := NewGlobalKV[string](s, "stats")
	require.NoError(t, err)

	// 3 live, 1 expired. Use a tiny TTL plus sleep to avoid time skew.
	require.NoError(t, kv.Put(ctx, "a", "1"))
	require.NoError(t, kv.Put(ctx, "b", "2"))
	require.NoError(t, kv.PutWithTTL(ctx, "c", "3", time.Hour))
	require.NoError(t, kv.PutWithTTL(ctx, "expired", "x", 5*time.Millisecond))
	time.Sleep(20 * time.Millisecond)

	got, err := s.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, len(loadSqliteMigrations()), got.SchemaVer)
	require.True(t, got.SizeBytes > 0)
	require.Len(t, got.Primitives, 1)

	kvStats := got.Primitives[0]
	require.Equal(t, "kv", kvStats.Name)
	require.Equal(t, int64(4), kvStats.RowCount)
	require.Equal(t, int64(1), kvStats.ExpiredRows)
	require.NotNil(t, kvStats.OldestEntry)
	require.NotNil(t, kvStats.NewestEntry)
}

func TestStats_EmptyDB(t *testing.T) {
	s := newTestStorage(t)
	got, err := s.Stats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), got.Primitives[0].RowCount)
	require.Nil(t, got.Primitives[0].OldestEntry)
	require.Nil(t, got.Primitives[0].NewestEntry)
}
