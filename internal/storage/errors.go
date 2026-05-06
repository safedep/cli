package storage

import "errors"

// Sentinel errors returned by the storage layer. Use errors.Is to
// match. Callers may wrap with extra context, except ErrSchemaTooNew
// whose message is meant to surface verbatim to the user.
var (
	ErrNotFound     = errors.New("storage: not found")
	ErrSchemaTooNew = errors.New("storage: db schema is newer than this binary: upgrade the CLI or run `safedep cleanup --reset`")
)
