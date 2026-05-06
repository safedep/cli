# Storage KV

A typed key-value primitive over the CLI's local sqlite store. Use it
for small command-specific state: last-used tenant, schema cache,
operation timestamps, etc.

## When to use

- The state survives across CLI invocations.
- It is small (kilobytes, not megabytes).
- It is not credential material (use the keychain via `dry/cloud`).
- Per-entry value fits in a JSON document.

For raw bytes at scale or schema-rich queries, propose a new primitive
in `internal/storage`; do not abuse KV.

## Accessors

KV stores are obtained from `*app.App` via free generic functions
(Go does not allow type parameters on methods):

```go
import "github.com/safedep/cli/internal/app"

kv, err := app.ProfileKV[T](a, "<namespace>")  // scoped to active profile
kv, err := app.GlobalKV[T](a, "<namespace>")   // unscoped
```

`T` is any `encoding/json`-serialisable Go type. Namespace must match
`^[a-z][a-z0-9_-]{0,63}$`.

### Scope choice

| Use case | Accessor |
|---|---|
| Per-profile state (last-used tenant, query cache, auth-flow flags) | `ProfileKV` |
| Global preferences (output format, telemetry consent) | `GlobalKV` |

A `ProfileKV` for one profile cannot read a different profile's data.
A `ProfileKV` cannot read a `GlobalKV` and vice versa. Scope is
structural; you cannot leak it by accident.

## API

```go
type KV[T any] struct{ /* opaque */ }

type Entry[T any] struct {
    Key       string
    Value     T
    CreatedAt time.Time
    UpdatedAt time.Time
    ExpiresAt *time.Time // nil = no TTL
}

func (kv *KV[T]) Get(ctx, key) (T, error)               // ErrNotFound if missing or expired
func (kv *KV[T]) GetEntry(ctx, key) (Entry[T], error)
func (kv *KV[T]) Put(ctx, key, value) error             // upsert
func (kv *KV[T]) PutWithTTL(ctx, key, value, ttl) error // ttl > 0
func (kv *KV[T]) Delete(ctx, key) error                 // no-op if missing
func (kv *KV[T]) Has(ctx, key) (bool, error)
func (kv *KV[T]) List(ctx) ([]Entry[T], error)
```

`Get`, `GetEntry`, `Has`, and `List` filter out expired rows even before
the cleanup command runs.

## Example

```go
package auth

import (
    "context"
    "errors"

    "github.com/safedep/cli/internal/app"
    "github.com/safedep/cli/internal/storage"
)

type lastTenant struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

func RememberTenant(ctx context.Context, a *app.App, t lastTenant) error {
    kv, err := app.ProfileKV[lastTenant](a, "auth")
    if err != nil {
        return err
    }
    return kv.Put(ctx, "last-tenant", t)
}

func RecallTenant(ctx context.Context, a *app.App) (lastTenant, bool, error) {
    kv, err := app.ProfileKV[lastTenant](a, "auth")
    if err != nil {
        return lastTenant{}, false, err
    }
    t, err := kv.Get(ctx, "last-tenant")
    if errors.Is(err, storage.ErrNotFound) {
        return lastTenant{}, false, nil
    }
    if err != nil {
        return lastTenant{}, false, err
    }
    return t, true, nil
}
```

## Encoding

Values are JSON-encoded under the hood. Scalars (`string`, `int64`,
`bool`), `[]byte`, structs, slices, and maps round-trip cleanly.
`[]byte` is base64-encoded inside the JSON document; the BLOB on disk
is the raw JSON bytes. The encoding is invisible to callers.

## TTL and cleanup

- `PutWithTTL(ctx, key, value, ttl)` writes `expires_at = now + ttl`.
- Reads ignore expired rows; they remain on disk until cleanup runs.
- `safedep cleanup` (future command) deletes expired rows and applies
  per-primitive retention. Configure overrides under
  `storage.retention.kv` in the CLI config.

## Sentinel errors

```go
import "github.com/safedep/cli/internal/storage"

errors.Is(err, storage.ErrNotFound)     // missing or expired
errors.Is(err, storage.ErrSchemaTooNew) // DB written by a newer CLI
```

## Boundaries

- Commands obtain KV instances only via `app.ProfileKV` /
  `app.GlobalKV`. Do not call `storage.Open` or `storage.NewProfileKV`
  / `storage.NewGlobalKV` directly outside `internal/app`.
- All raw SQL lives in `internal/storage`. Commands never see SQL.
- The sqlite backend is encapsulated; a future Postgres or MySQL
  swap (per ADR Storage) does not change this API.
