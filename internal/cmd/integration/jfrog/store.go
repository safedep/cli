// Package jfrog provides integration with JFrog Artifactory.
package jfrog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type cursorStore struct {
	path string
}

type cursorState struct {
	LastSeenAt time.Time `json:"last_seen_at"`
}

func newCursorStore(path string) *cursorStore {
	return &cursorStore{path: path}
}

func (s *cursorStore) Load() (time.Time, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("cursor: read: %w", err)
	}

	var state cursorState
	if err := json.Unmarshal(data, &state); err != nil {
		return time.Time{}, fmt.Errorf("cursor: parse: %w", err)
	}

	return state.LastSeenAt, nil
}

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
