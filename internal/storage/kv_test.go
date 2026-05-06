package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sample struct {
	A int    `json:"a"`
	B string `json:"b"`
}

func TestKV_Put_Get_RoundTripStruct(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewProfileKV[sample](s, "default", "test")
	require.NoError(t, err)

	want := sample{A: 42, B: "hello"}
	require.NoError(t, kv.Put(context.Background(), "k", want))

	got, err := kv.Get(context.Background(), "k")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestKV_RoundTrip_ScalarsAndCollections(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	t.Run("string", func(t *testing.T) {
		kv, err := NewGlobalKV[string](s, "scalars-string")
		require.NoError(t, err)
		require.NoError(t, kv.Put(ctx, "k", "value"))
		got, err := kv.Get(ctx, "k")
		require.NoError(t, err)
		require.Equal(t, "value", got)
	})

	t.Run("int64", func(t *testing.T) {
		kv, err := NewGlobalKV[int64](s, "scalars-int64")
		require.NoError(t, err)
		require.NoError(t, kv.Put(ctx, "k", int64(-7)))
		got, err := kv.Get(ctx, "k")
		require.NoError(t, err)
		require.Equal(t, int64(-7), got)
	})

	t.Run("bool", func(t *testing.T) {
		kv, err := NewGlobalKV[bool](s, "scalars-bool")
		require.NoError(t, err)
		require.NoError(t, kv.Put(ctx, "k", true))
		got, err := kv.Get(ctx, "k")
		require.NoError(t, err)
		require.True(t, got)
	})

	t.Run("bytes", func(t *testing.T) {
		kv, err := NewGlobalKV[[]byte](s, "scalars-bytes")
		require.NoError(t, err)
		require.NoError(t, kv.Put(ctx, "k", []byte{1, 2, 3}))
		got, err := kv.Get(ctx, "k")
		require.NoError(t, err)
		require.Equal(t, []byte{1, 2, 3}, got)
	})

	t.Run("slice", func(t *testing.T) {
		kv, err := NewGlobalKV[[]string](s, "scalars-slice")
		require.NoError(t, err)
		require.NoError(t, kv.Put(ctx, "k", []string{"a", "b"}))
		got, err := kv.Get(ctx, "k")
		require.NoError(t, err)
		require.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("map", func(t *testing.T) {
		kv, err := NewGlobalKV[map[string]int](s, "scalars-map")
		require.NoError(t, err)
		require.NoError(t, kv.Put(ctx, "k", map[string]int{"x": 1}))
		got, err := kv.Get(ctx, "k")
		require.NoError(t, err)
		require.Equal(t, map[string]int{"x": 1}, got)
	})
}

func TestKV_Get_MissingReturnsErrNotFound(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "missing")
	require.NoError(t, err)

	_, err = kv.Get(context.Background(), "absent")
	require.True(t, errors.Is(err, ErrNotFound))
}

func TestKV_Has_MissingFalse(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "has")
	require.NoError(t, err)
	got, err := kv.Has(context.Background(), "absent")
	require.NoError(t, err)
	require.False(t, got)
}

func TestKV_Put_IsUpsert_PreservesCreatedAt(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "upsert")
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, kv.Put(ctx, "k", "v1"))
	first, err := kv.GetEntry(ctx, "k")
	require.NoError(t, err)

	// Sleep a touch so updated_at can differ.
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, kv.Put(ctx, "k", "v2"))

	second, err := kv.GetEntry(ctx, "k")
	require.NoError(t, err)
	require.Equal(t, "v2", second.Value)
	require.Equal(t, first.CreatedAt, second.CreatedAt, "created_at must be preserved on upsert")
	require.True(t, second.UpdatedAt.After(first.UpdatedAt) || second.UpdatedAt.Equal(first.UpdatedAt))
}

func TestKV_Delete_NoopOnMissing(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "del")
	require.NoError(t, err)
	require.NoError(t, kv.Delete(context.Background(), "never-existed"))
}

func TestKV_List_OnlyReturnsScopeNamespace(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	a, err := NewGlobalKV[string](s, "ns-a")
	require.NoError(t, err)
	b, err := NewGlobalKV[string](s, "ns-b")
	require.NoError(t, err)

	require.NoError(t, a.Put(ctx, "k1", "v1"))
	require.NoError(t, a.Put(ctx, "k2", "v2"))
	require.NoError(t, b.Put(ctx, "z", "z"))

	listA, err := a.List(ctx)
	require.NoError(t, err)
	require.Len(t, listA, 2)

	listB, err := b.List(ctx)
	require.NoError(t, err)
	require.Len(t, listB, 1)
}

func TestKV_TTL_ExpiredIsInvisible(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "ttl")
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, kv.PutWithTTL(ctx, "soon", "v", 5*time.Millisecond))
	time.Sleep(20 * time.Millisecond)

	_, err = kv.Get(ctx, "soon")
	require.True(t, errors.Is(err, ErrNotFound))

	has, err := kv.Has(ctx, "soon")
	require.NoError(t, err)
	require.False(t, has)

	list, err := kv.List(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestKV_TTL_FutureIsVisibleWithExpiresAt(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "ttl-fut")
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, kv.PutWithTTL(ctx, "k", "v", time.Hour))
	e, err := kv.GetEntry(ctx, "k")
	require.NoError(t, err)
	require.NotNil(t, e.ExpiresAt)
	require.True(t, e.ExpiresAt.After(time.Now()))
}

func TestKV_PutWithTTL_RejectsNonPositive(t *testing.T) {
	s := newTestStorage(t)
	kv, err := NewGlobalKV[string](s, "ttl-rej")
	require.NoError(t, err)

	require.Error(t, kv.PutWithTTL(context.Background(), "k", "v", 0))
	require.Error(t, kv.PutWithTTL(context.Background(), "k", "v", -time.Second))
}

func TestKV_ScopeIsolation_ProfilesAreSeparate(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	x, err := NewProfileKV[string](s, "x", "auth")
	require.NoError(t, err)
	y, err := NewProfileKV[string](s, "y", "auth")
	require.NoError(t, err)

	require.NoError(t, x.Put(ctx, "k", "x-data"))

	_, err = y.Get(ctx, "k")
	require.True(t, errors.Is(err, ErrNotFound))
}

func TestKV_ScopeIsolation_GlobalSeparateFromProfile(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	g, err := NewGlobalKV[string](s, "iso")
	require.NoError(t, err)
	p, err := NewProfileKV[string](s, "default", "iso")
	require.NoError(t, err)

	require.NoError(t, g.Put(ctx, "k", "global"))
	_, err = p.Get(ctx, "k")
	require.True(t, errors.Is(err, ErrNotFound))
}

func TestKV_NamespaceValidation(t *testing.T) {
	s := newTestStorage(t)

	cases := []string{
		"",
		"UPPER",
		" leading-space",
		"with space",
		"123starts-with-digit",
		"toolong-toolong-toolong-toolong-toolong-toolong-toolong-toolong-toolong",
	}
	for _, c := range cases {
		_, err := NewGlobalKV[string](s, c)
		assert.Errorf(t, err, "expected rejection for namespace %q", c)
	}

	_, err := NewGlobalKV[string](s, "ok-name_1")
	require.NoError(t, err)
}

func TestKV_ProfileValidation_RejectsEmpty(t *testing.T) {
	s := newTestStorage(t)
	_, err := NewProfileKV[string](s, "", "ns")
	require.Error(t, err)
}
