package endpoint

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectory_resolveByULID(t *testing.T) {
	d := newTestDirectory(t)
	id, err := d.Resolve(context.Background(), "01KR0EKN6PMW0ZRFRN992H1PKX")
	require.NoError(t, err)
	assert.Equal(t, "01KR0EKN6PMW0ZRFRN992H1PKX", id)
}

func TestDirectory_resolveByName(t *testing.T) {
	d := newTestDirectory(t)
	require.NoError(t, d.Upsert(context.Background(), []DirectoryEntry{{
		ID: "01KR0EKN6PMW0ZRFRN992H1PKX", Hostname: "laptop-abhi",
	}}))
	id, err := d.Resolve(context.Background(), "laptop-abhi")
	require.NoError(t, err)
	assert.Equal(t, "01KR0EKN6PMW0ZRFRN992H1PKX", id)
}

func TestDirectory_resolveAmbiguous(t *testing.T) {
	d := newTestDirectory(t)
	require.NoError(t, d.Upsert(context.Background(), []DirectoryEntry{
		{ID: "01KR0EKN6PMW0ZRFRN992H1PKX", Hostname: "mac"},
		{ID: "01KR0EKN6PMW0ZRFRN992H1PKY", Hostname: "mac"},
	}))
	_, err := d.Resolve(context.Background(), "mac")
	require.Error(t, err)
	var amb *AmbiguousRefError
	require.ErrorAs(t, err, &amb)
	assert.Len(t, amb.Candidates, 2)
}

func TestDirectory_resolveNotFound(t *testing.T) {
	d := newTestDirectory(t)
	_, err := d.Resolve(context.Background(), "ghost")
	require.ErrorIs(t, err, ErrEndpointNotInDirectory)
}

func TestDirectory_resolveByULIDPrefix(t *testing.T) {
	d := newTestDirectory(t)
	require.NoError(t, d.Upsert(context.Background(), []DirectoryEntry{{
		ID: "01KR0EKN6PMW0ZRFRN992H1PKX", Hostname: "lap",
	}}))
	id, err := d.Resolve(context.Background(), "01KR0EKN6PMW0")
	require.NoError(t, err)
	assert.Equal(t, "01KR0EKN6PMW0ZRFRN992H1PKX", id)
}

func TestDirectory_resolveAmbiguousPrefix(t *testing.T) {
	d := newTestDirectory(t)
	require.NoError(t, d.Upsert(context.Background(), []DirectoryEntry{
		{ID: "01KR0EKN6PMW0ZRFRN992H1PKX", Hostname: "a"},
		{ID: "01KR0EKN6PMW0ZRFRN992H1PKY", Hostname: "b"},
	}))
	_, err := d.Resolve(context.Background(), "01KR0EKN6PMW0")
	var amb *AmbiguousRefError
	require.ErrorAs(t, err, &amb)
	assert.Len(t, amb.Candidates, 2)
}

func TestDirectory_resolveShortPrefixSkipped(t *testing.T) {
	d := newTestDirectory(t)
	require.NoError(t, d.Upsert(context.Background(), []DirectoryEntry{{
		ID: "01KR0EKN6PMW0ZRFRN992H1PKX", Hostname: "lap",
	}}))
	_, err := d.Resolve(context.Background(), "01K")
	require.ErrorIs(t, err, ErrEndpointNotInDirectory)
}

func TestDirectory_expiredEntriesIgnored(t *testing.T) {
	d := newTestDirectoryWithClock(t, func() time.Time { return time.Unix(2_000_000_000, 0) })
	require.NoError(t, d.Upsert(context.Background(), []DirectoryEntry{{
		ID: "01KR0EKN6PMW0ZRFRN992H1PKX", Hostname: "old", CachedAt: time.Unix(1_000_000_000, 0),
	}}))
	_, err := d.Resolve(context.Background(), "old")
	require.ErrorIs(t, err, ErrEndpointNotInDirectory)
}

func TestDirectory_zeroCachedAtEntriesExpire(t *testing.T) {
	store := &fakeStore{data: map[string]DirectoryEntry{
		"01KR0EKN6PMW0ZRFRN992H1PKX": {
			ID:       "01KR0EKN6PMW0ZRFRN992H1PKX",
			Hostname: "legacy-host",
		},
	}}
	d := NewDirectory(store, func() time.Time { return time.Unix(2_000_000_000, 0) })

	_, err := d.Resolve(context.Background(), "legacy-host")
	require.ErrorIs(t, err, ErrEndpointNotInDirectory)

	_, ok := d.Lookup(context.Background(), "01KR0EKN6PMW0ZRFRN992H1PKX")
	assert.False(t, ok)
}

type fakeStore struct{ data map[string]DirectoryEntry }

func newFakeStore() *fakeStore { return &fakeStore{data: map[string]DirectoryEntry{}} }
func (f *fakeStore) Get(_ context.Context) (map[string]DirectoryEntry, error) { return f.data, nil }
func (f *fakeStore) Put(_ context.Context, v map[string]DirectoryEntry) error {
	f.data = v
	return nil
}

func newTestDirectory(t *testing.T) *Directory {
	t.Helper()
	return NewDirectory(newFakeStore(), time.Now)
}

func newTestDirectoryWithClock(t *testing.T, clock func() time.Time) *Directory {
	t.Helper()
	return NewDirectory(newFakeStore(), clock)
}
