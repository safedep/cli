package storage

import "errors"

// Sentinel errors returned by the storage layer. Use errors.Is to
// match. Callers may wrap with extra context, except ErrSchemaTooNew
// whose message is meant to surface verbatim to the user.
var (
	ErrNotFound     = errors.New("storage: not found")
	ErrSchemaTooNew = errors.New("storage: db schema is newer than this binary: upgrade the CLI or run `safedep cleanup --reset`")
	// ErrKVDecode is returned by KV.Get and KV.List when the stored value
	// cannot be JSON-decoded into the target type. Callers can distinguish
	// this from a DB-level error (e.g. locked file, permission denied) via
	// errors.Is to decide whether a reset is safe.
	ErrKVDecode = errors.New("storage: kv decode")
)
