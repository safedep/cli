package endpoint

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
// production implementation wraps app.ProfileKV. Tests pass an
// in-memory fake.
type Store interface {
	Get(ctx context.Context) (map[string]DirectoryEntry, error)
	Put(ctx context.Context, v map[string]DirectoryEntry) error
}

type Directory struct {
	store Store
	now   func() time.Time

	mu     sync.Mutex
	cache  map[string]DirectoryEntry
	loaded bool
}

// loadCache memoises the underlying store fetch for the lifetime of
// this Directory. The CLI builds one Directory per command invocation
// so a single sqlite read covers every Resolve/Lookup call.
func (d *Directory) loadCache(ctx context.Context) (map[string]DirectoryEntry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loaded {
		return d.cache, nil
	}
	c, err := d.store.Get(ctx)
	if err != nil {
		return nil, err
	}
	d.cache = c
	d.loaded = true
	return c, nil
}

func (d *Directory) setCache(c map[string]DirectoryEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache = c
	d.loaded = true
}

func NewDirectory(store Store, now func() time.Time) *Directory {
	if now == nil {
		now = time.Now
	}
	return &Directory{store: store, now: now}
}

// minULIDPrefix is the shortest input we treat as a ULID-prefix match.
// Below this length the prefix is too ambiguous to be useful and we
// prefer the not-found error.
const minULIDPrefix = 4

// Resolve maps a CLI-supplied reference to an endpoint ID. Accepts a
// full ULID, a unique ULID prefix (like `git` short SHAs), or a cached
// hostname/identifier. Errors with AmbiguousRefError when the reference
// matches multiple cached endpoints.
func (d *Directory) Resolve(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("endpoint reference is required")
	}
	if isULID(ref) {
		return ref, nil
	}
	cache, err := d.loadCache(ctx)
	if err != nil {
		return "", err
	}
	tryPrefix := len(ref) >= minULIDPrefix
	var byName, byPrefix []DirectoryEntry
	for _, e := range cache {
		if d.expired(e) {
			continue
		}
		if strings.EqualFold(e.Hostname, ref) || strings.EqualFold(e.Name, ref) {
			byName = append(byName, e)
		}
		if tryPrefix && strings.HasPrefix(e.ID, ref) {
			byPrefix = append(byPrefix, e)
		}
	}
	switch {
	case len(byName) == 1:
		return byName[0].ID, nil
	case len(byName) > 1:
		return "", &AmbiguousRefError{Ref: ref, Candidates: byName}
	case len(byPrefix) == 1:
		return byPrefix[0].ID, nil
	case len(byPrefix) > 1:
		return "", &AmbiguousRefError{Ref: ref, Candidates: byPrefix}
	default:
		return "", ErrEndpointNotInDirectory
	}
}

// Upsert merges entries into the cached directory, stamping CachedAt
// for any entry whose CachedAt is zero.
func (d *Directory) Upsert(ctx context.Context, entries []DirectoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	cache, err := d.loadCache(ctx)
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
	if err := d.store.Put(ctx, cache); err != nil {
		return err
	}
	d.setCache(cache)
	return nil
}

// Lookup returns the cached entry for an ID, or false.
func (d *Directory) Lookup(ctx context.Context, id string) (DirectoryEntry, bool) {
	cache, err := d.loadCache(ctx)
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

// kvStore adapts app.ProfileKV to the Store interface. The whole
// directory lives under a single "all" key. Per-entry TTL would not
// help here. Expiry is enforced in memory by Directory.expired.
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
