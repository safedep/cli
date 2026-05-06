package app

import (
	"context"
	"fmt"
	"time"

	"github.com/safedep/cli/internal/config"
	"github.com/safedep/cli/internal/storage"
)

// Storage returns the lazy-initialised CLI storage layer. Open uses a
// process-scoped context for migration work; per-call operations
// should pass the cobra command's context to the primitive methods.
func (a *App) Storage() (storage.Storage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.storage != nil {
		return a.storage, nil
	}

	path, err := config.DBPath()
	if err != nil {
		return nil, fmt.Errorf("app: storage path: %w", err)
	}

	s, err := storage.Open(context.Background(), storage.Options{
		Backend:     storage.BackendSqlite,
		Path:        path,
		BusyTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	a.storage = s
	return s, nil
}

// ProfileKV returns a typed KV store scoped to the active profile.
// Free function because Go does not allow type parameters on methods.
func ProfileKV[T any](a *App, namespace string) (*storage.KV[T], error) {
	s, err := a.Storage()
	if err != nil {
		return nil, err
	}
	return storage.NewProfileKV[T](s, a.Profile(), namespace)
}

// GlobalKV returns a typed KV store in the global (profile-independent)
// scope.
func GlobalKV[T any](a *App, namespace string) (*storage.KV[T], error) {
	s, err := a.Storage()
	if err != nil {
		return nil, err
	}
	return storage.NewGlobalKV[T](s, namespace)
}
