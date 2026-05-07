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
)

// cursorKey is the single KV key used to store the poll cursor. One key
// per namespace keeps the store simple; if per-ecosystem cursors are
// ever needed, a scheme like "cursor:<ecosystem>" can be introduced.
const cursorKey = "cursor"

// cursorStore wraps the typed KV store so the poller does not need to
// know about KV internals. It persists the timestamp of the last
// processed analysis record so the daemon can resume where it left off
// across restarts.
//
// The underlying KV store is profile-scoped (obtained via
// app.ProfileKV), so each SafeDep credential profile has an independent
// cursor. Switching --profile automatically switches the cursor.
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

// Load returns the persisted cursor, or zero time if no cursor has been
// saved yet (first run). A missing key is "start fresh", not an error.
func (s *cursorStore) Load(ctx context.Context) (time.Time, error) {
	state, err := s.kv.Get(ctx, cursorKey)
	if errors.Is(err, storage.ErrNotFound) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("cursor: get: %w", err)
	}
	return state.LastSeenAt, nil
}

// Save persists the cursor. KV Put is an upsert, so there is no
// separate "create on first run" path.
func (s *cursorStore) Save(ctx context.Context, t time.Time) error {
	if err := s.kv.Put(ctx, cursorKey, cursorState{LastSeenAt: t}); err != nil {
		return fmt.Errorf("cursor: put: %w", err)
	}
	return nil
}
