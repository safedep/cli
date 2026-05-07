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
	"github.com/safedep/dry/log"
)

// cursorKey is the single KV key used to store the poll cursor.
const cursorKey = "cursor"

// cursorStore wraps the typed KV store so the poller does not need to
// know about KV internals. It persists the timestamp of the last
// processed analysis record so the daemon can resume where it left off
// across restarts.
//
// The underlying KV store is profile-scoped (obtained via app.ProfileKV),
// so each SafeDep credential profile has an independent cursor.
// Switching --profile automatically switches the cursor.
type cursorStore struct {
	kv *storage.KV[time.Time]
}

func newCursorStore(kv *storage.KV[time.Time]) *cursorStore {
	return &cursorStore{kv: kv}
}

// Load returns the persisted cursor, or zero time on first run.
// An incompatible stored value (e.g. leftover from a previous schema) is
// deleted and treated as "start fresh" so a one-time format migration never
// blocks the daemon.
func (s *cursorStore) Load(ctx context.Context) (time.Time, error) {
	t, err := s.kv.Get(ctx, cursorKey)
	if errors.Is(err, storage.ErrNotFound) {
		return time.Time{}, nil
	}
	if err != nil {
		log.Warnf("cursor: incompatible value, resetting: %v", err)
		_ = s.kv.Delete(ctx, cursorKey)
		return time.Time{}, nil
	}
	return t, nil
}

// Save persists the cursor. KV Put is an upsert.
func (s *cursorStore) Save(ctx context.Context, t time.Time) error {
	if err := s.kv.Put(ctx, cursorKey, t); err != nil {
		return fmt.Errorf("cursor: put: %w", err)
	}
	return nil
}
