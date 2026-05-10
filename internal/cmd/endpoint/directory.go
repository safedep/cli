package endpoint

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/storage"
)

const directoryTTL = 30 * 24 * time.Hour

var ErrEndpointNotInDirectory = errors.New("endpoint not found in local directory; run `safedep endpoint list` first or pass a ULID")

type AmbiguousRefError struct {
	Ref        string
	Candidates []DirectoryEntry
}

func (e *AmbiguousRefError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d endpoints; pass an ID:", e.Ref, len(e.Candidates))
	for _, c := range e.Candidates {
		fmt.Fprintf(&b, "\n  %s  %s", c.ID, c.Hostname)
	}
	return b.String()
}

type DirectoryEntry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	Hostname   string    `json:"hostname,omitempty"`
	LastSyncAt time.Time `json:"last_sync_at,omitempty"`
	CachedAt   time.Time `json:"cached_at"`
}

// Store is the small persistence surface the Directory needs. The
// production implementation wraps app.ProfileKV[map[string]DirectoryEntry];
// tests pass an in-memory fake.
type Store interface {
	Get(ctx context.Context) (map[string]DirectoryEntry, error)
	Put(ctx context.Context, v map[string]DirectoryEntry) error
}

type Directory struct {
	store Store
	now   func() time.Time
}

func NewDirectory(store Store, now func() time.Time) *Directory {
	if now == nil {
		now = time.Now
	}
	return &Directory{store: store, now: now}
}

// Resolve maps a CLI-supplied reference (ULID or hostname/name) to an
// endpoint ID. ULIDs short-circuit; names search the cache.
func (d *Directory) Resolve(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("endpoint reference is required")
	}
	if isULID(ref) {
		return ref, nil
	}
	cache, err := d.store.Get(ctx)
	if err != nil {
		return "", err
	}
	refLower := strings.ToLower(ref)
	var matches []DirectoryEntry
	for _, e := range cache {
		if d.expired(e) {
			continue
		}
		if strings.EqualFold(e.Hostname, refLower) || strings.EqualFold(e.Name, refLower) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return "", ErrEndpointNotInDirectory
	case 1:
		return matches[0].ID, nil
	default:
		return "", &AmbiguousRefError{Ref: ref, Candidates: matches}
	}
}

// Upsert merges entries into the cached directory, stamping CachedAt
// for any entry whose CachedAt is zero.
func (d *Directory) Upsert(ctx context.Context, entries []DirectoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	cache, err := d.store.Get(ctx)
	if err != nil {
		return err
	}
	if cache == nil {
		cache = map[string]DirectoryEntry{}
	}
	now := d.now()
	for _, e := range entries {
		if e.CachedAt.IsZero() {
			e.CachedAt = now
		}
		cache[e.ID] = e
	}
	return d.store.Put(ctx, cache)
}

// Lookup returns the cached entry for an ID, or false.
func (d *Directory) Lookup(ctx context.Context, id string) (DirectoryEntry, bool) {
	cache, err := d.store.Get(ctx)
	if err != nil || cache == nil {
		return DirectoryEntry{}, false
	}
	e, ok := cache[id]
	if !ok || d.expired(e) {
		return DirectoryEntry{}, false
	}
	return e, true
}

func (d *Directory) expired(e DirectoryEntry) bool {
	if e.CachedAt.IsZero() {
		return false
	}
	return d.now().Sub(e.CachedAt) > directoryTTL
}

// kvStore adapts app.ProfileKV[map[string]DirectoryEntry] to the
// Store interface. Each Get loads the single "all" key.
type kvStore struct{ kv *storage.KV[map[string]DirectoryEntry] }

func (k *kvStore) Get(ctx context.Context) (map[string]DirectoryEntry, error) {
	v, err := k.kv.Get(ctx, "all")
	if errors.Is(err, storage.ErrNotFound) {
		return map[string]DirectoryEntry{}, nil
	}
	return v, err
}

func (k *kvStore) Put(ctx context.Context, v map[string]DirectoryEntry) error {
	return k.kv.Put(ctx, "all", v)
}

// NewDirectoryFromApp builds a directory backed by the per-profile KV.
func NewDirectoryFromApp(a *app.App) (*Directory, error) {
	kv, err := app.ProfileKV[map[string]DirectoryEntry](a, "endpoint-directory")
	if err != nil {
		return nil, err
	}
	return NewDirectory(&kvStore{kv: kv}, time.Now), nil
}
