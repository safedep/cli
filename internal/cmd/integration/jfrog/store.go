// Package jfrog implements the SafeDep -> JFrog XRay malicious package
// integration. The package owns the cobra surface (cmd.go, run.go), the
// poll-and-push orchestration (service.go, poller.go, pusher.go) and the
// file-backed cursor that survives restarts (this file).
package jfrog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/safedep/dry/log"
)

// cursorStore persists the timestamp of the last processed analysis record so
// the daemon can resume where it left off across restarts. Operators can edit
// the file by hand to reprocess history.
type cursorStore struct {
	path string
}

// cursorState is the on-disk format. Exporting json tags keeps the file
// editable by humans without a CLI dance.
type cursorState struct {
	LastSeenAt time.Time `json:"last_seen_at"`
}

func newCursorStore(path string) *cursorStore {
	return &cursorStore{path: path}
}

// Load returns the persisted cursor, or the zero time if the file does not
// exist or is empty/corrupt. A corrupt file is treated as "start fresh" with
// a warning so a manually emptied or truncated file never blocks the daemon.
func (s *cursorStore) Load() (time.Time, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("cursor: read: %w", err)
	}
	if len(data) == 0 {
		log.Warnf("cursor: file %s is empty, starting from beginning", s.path)
		return time.Time{}, nil
	}

	var state cursorState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Warnf("cursor: file %s is corrupt (%v), starting from beginning", s.path, err)
		return time.Time{}, nil
	}

	return state.LastSeenAt, nil
}

// Save writes the cursor atomically (write-temp, rename) so a crash mid-write
// cannot leave a corrupted file that would block the next start.
func (s *cursorStore) Save(t time.Time) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("cursor: mkdir: %w", err)
	}

	data, err := json.Marshal(cursorState{LastSeenAt: t})
	if err != nil {
		return fmt.Errorf("cursor: marshal: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("cursor: write tmp: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("cursor: rename: %w", err)
	}

	return nil
}
