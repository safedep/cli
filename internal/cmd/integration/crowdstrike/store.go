package crowdstrike

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safedep/cli/internal/storage"
	drytui "github.com/safedep/dry/tui"
)

// cursorStore wraps the typed KV store so the source does not need to know
// about KV internals. It persists the watermark of the last processed event so
// the sync can resume where it left off across restarts.
//
// The underlying KV store is profile-scoped (obtained via app.ProfileKV), so
// each SafeDep credential profile has an independent cursor. Switching
// --profile automatically switches the cursor.
type cursorStore struct {
	kv *storage.KV[cursorState]
}

// cursorState is the value stored per key. Endpoint block events are immutable
// and append-only, so a Timestamp watermark is the natural cursor. LastSeenEventIDs
// holds the ids of every event at exactly LastSeenAt: the API does not document
// whether TimeRange.Start is inclusive, so those ids are skipped on the next
// cycle to avoid re-delivering the boundary.
type cursorState struct {
	LastSeenAt       time.Time `json:"last_seen_at"`
	LastSeenEventIDs []string  `json:"last_seen_event_ids"`
}

func newCursorStore(kv *storage.KV[cursorState]) *cursorStore {
	return &cursorStore{kv: kv}
}

// load returns the persisted cursor, or the zero value on first run.
//
// Only JSON decode failures (storage.ErrKVDecode) are treated as an
// incompatible format: the stale key is deleted so the next write starts clean.
// DB-level errors (locked file, permission denied) are propagated so the caller
// can retry on the next cycle rather than silently destroying a valid cursor.
func (s *cursorStore) load(ctx context.Context) (cursorState, error) {
	t, err := s.kv.Get(ctx, kvCursorKey)
	if errors.Is(err, storage.ErrNotFound) {
		return cursorState{}, nil
	}
	if errors.Is(err, storage.ErrKVDecode) {
		drytui.Warning("Cursor value incompatible, resetting to beginning: %v", err)
		_ = s.kv.Delete(ctx, kvCursorKey)
		return cursorState{}, nil
	}
	if err != nil {
		return cursorState{}, fmt.Errorf("cursor: get: %w", err)
	}
	return t, nil
}

// save persists the cursor. KV Put is an upsert.
func (s *cursorStore) save(ctx context.Context, state cursorState) error {
	if err := s.kv.Put(ctx, kvCursorKey, state); err != nil {
		return fmt.Errorf("cursor: put: %w", err)
	}
	return nil
}

// remove deletes the stored cursor so the next run starts fresh. Deleting a
// missing key is a no-op.
func (s *cursorStore) remove(ctx context.Context) error {
	if err := s.kv.Delete(ctx, kvCursorKey); err != nil {
		return fmt.Errorf("cursor: delete: %w", err)
	}
	return nil
}
