// Package jfrog implements the SafeDep -> JFrog XRay malicious package
// integration. The package owns the cobra surface (cmd.go, run.go), the
// poll-and-push orchestration (service.go, poller.go, pusher.go) and the
// KV-backed cursor that survives restarts (this file).
package jfrog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safedep/cli/internal/storage"
	drytui "github.com/safedep/dry/tui"
)

// cursorStore wraps the typed KV store so the poller does not need to
// know about KV internals. It persists the timestamp of the last
// processed analysis record so the daemon can resume where it left off
// across restarts.
//
// The underlying KV store is profile-scoped (obtained via app.ProfileKV),
// so each SafeDep credential profile has an independent cursor.
// Switching --profile automatically switches the cursor.
type cursorStore struct {
	kv *storage.KV[cursorState]
}

// cursorState is the value stored per key. A struct (rather than a bare
// time.Time) keeps the JSON document extensible without a migration.
type cursorState struct {
	LastSeenAt time.Time `json:"last_seen_at"`
}

func newCursorStore(kv *storage.KV[cursorState]) *cursorStore {
	return &cursorStore{kv: kv}
}

// Load returns the persisted cursor, or zero time on first run.
//
// Only JSON decode failures (storage.ErrKVDecode) are treated as an
// incompatible format — the stale key is deleted so the next write starts
// clean. DB-level errors (locked file, permission denied, etc.) are
// propagated so the caller can retry on the next poll cycle rather than
// silently destroying a cursor that may still be valid.
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

// Save persists the cursor. KV Put is an upsert.
func (s *cursorStore) save(ctx context.Context, state cursorState) error {
	if err := s.kv.Put(ctx, kvCursorKey, state); err != nil {
		return fmt.Errorf("cursor: put: %w", err)
	}
	return nil
}
