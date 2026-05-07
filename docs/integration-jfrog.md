# JFrog Integration: Developer Guide

Architecture and extension points for `internal/cmd/integration/jfrog/`.
For end-user docs see [`cmd/integration-jfrog-run.md`](./cmd/integration-jfrog-run.md).

## Pieces

| File | Responsibility |
|---|---|
| `cmd.go` | Cobra registration. |
| `run.go` | CLI flag/env resolution → `resolveConfig`. Constructs source + pusher and hands to `feedService`. |
| `types.go` | In-memory DTOs (`Config`, `SourceConfig`, `JFrogConfig`). No on-disk schema. |
| `source.go` | `packageSource` interface and `recordHandler` callback type. |
| `source_poll.go` | `pollSource`: gRPC pull implementation backed by a KV cursor. |
| `poller.go` | Single-cycle gRPC pagination + cursor advance. Used only by `pollSource`. |
| `store.go` | `cursorStore`: wraps `*storage.KV[time.Time]`. Used only by `pollSource`. |
| `pusher.go` | HTTP POST to JFrog XRay `/xray/api/v1/events`. Wire format and ID rules. |
| `validate.go` | Pre-flight URL + token check via `/xray/api/v1/policies`. |
| `service.go` | `feedService`: validates JFrog, then delegates to the source. |

## Data flow

```
SafeDep                   feedService                JFrog XRay
───────                   ───────────                ──────────
                              │
                              │ 1. validateJFrog (GET /policies)
                              │◀──────────────────────────────►
                              │
                              │ 2. source.Subscribe(ctx, push)
                              ▼
                       ┌──────────────┐
                       │ packageSource│
                       └──────┬───────┘
                              │
   ┌────────────────────┬─────┴───────────────┐
   │ pollSource         │  streamSource (TBD) │
   │  (gRPC pull)       │  (NATS / S2)        │
   └─────────┬──────────┴─────────────────────┘
             │ per record
             ▼
       recordHandler ──── pusher.Push ────► POST /xray/api/v1/events
                                            (Bearer token, JSON event)
```

## The `packageSource` contract

```go
type packageSource interface {
    Subscribe(ctx context.Context, onRecord recordHandler) error
}

type recordHandler func(*malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error
```

Implementations must:

- **Block** until `ctx` is cancelled. The daemon lifecycle is `ctx.Done()`,
  not `Subscribe` returning.
- Invoke `onRecord` **exactly once** per verified malicious package.
- Treat transient errors (gRPC blip, network reset, auth refresh) as
  retryable. Log via `drytui.Warning` and continue. Only return on
  fatal startup errors or context cancellation.
- Own their own resume state. **`feedService` knows nothing about how
  records are tracked.**

`recordHandler` errors stop further delivery for the current Subscribe
session; the source surfaces the error from Subscribe.

## Why this seam

The existing `pollSource` is one realisation. A future `streamSource`
backed by NATS / S2 JetStream is a drop-in alternative with completely
different state semantics:

| Concern | `pollSource` | `streamSource` (future) |
|---|---|---|
| Resume position | KV cursor (client-side) | Consumer offset (server-side) |
| Delivery loop | Sleep + re-poll | Block on subscription |
| Acknowledgment | Cursor advance after page | Per-message ack to broker |
| Network | gRPC unary | Persistent stream |
| Stale-state guard | 7-day API cutoff reset | (broker-defined retention) |

Nothing in `feedService`, `pusher`, or `validate` needs to change when a
new source lands.

## Adding a new source

1. Create `source_<name>.go` in this package.
2. Define a struct that implements `packageSource`.
3. The constructor takes whatever transport state is needed (NATS conn,
   JetStream consumer name, etc.). Keep it private. `run.go` does the
   wiring.
4. In `Subscribe`, drive your delivery loop until `ctx.Done()`. Call
   `onRecord` per record. Do not return on transient errors.
5. Wire it in `run.go` behind a flag (e.g. `--source poll|stream`) and
   default to `poll` until the new source is GA.
6. Add tests using a fake transport (see `poller_test.go` for the
   pattern with a fake gRPC client and a real `*storage.KV` from a
   temp DB).

## Cursor semantics (poll-source-only)

`pollSource` uses a profile-scoped KV under namespace `"integration-jfrog"`
with key `"cursor"`. The value is a JSON-encoded `time.Time` (RFC3339).

| Cursor state | Behaviour |
|---|---|
| Missing key (`storage.ErrNotFound`) | First run. `start_from` omitted; server uses `now − 1h` default. |
| Decode failure (`storage.ErrKVDecode`) | Stale schema. Delete + warn + start fresh. |
| DB error (locked, permission) | Propagate. The next poll cycle retries; never silently destroy. |
| Older than 7 days | API rejects `start_from` past cutoff. Reset to ~6 days ago + warn. |
| In-window | Used as `start_from`. **Constant for the entire pagination session.** |

The "constant for the session" rule is critical: SafeDep's
`next_page_token` is paired with a fixed `start_from`. Mutating
`start_from` between pages can invalidate the token.

## Logging boundary

Per [AGENTS.md](../AGENTS.md):

| Library | Use for | Examples in this package |
|---|---|---|
| `drytui` (`Info`, `Success`, `Warning`) | Operator-visible messages. State changes, errors the user can act on. | `Pushed: ...`, `Push failed for X`, `Cursor exceeds 7-day cutoff`, `Skipping record: nil package version` |
| `dry/log` (`Warnf`, etc.) | Internal diagnostics. Not actionable. | Deferred body close failure, bounded body read failure |

Get this wrong and either: (a) the user can't see what's happening (silent
`log.Warnf` in production where logs aren't surfaced) or (b) the terminal
fills with noise.

## Testing the wire format

`pusher_test.go` uses `httptest.NewServer` to capture requests and assert
the payload byte-for-byte against the JFrog reference. **Do not skip these
tests when changing the payload**. JFrog silently drops events that
violate any of:

- `id` length > 32 chars
- `id` starts with `"Xray"`
- `provider == "JFrog"`
- `vulnerable_versions` not in bracket notation (`["[1.0.5]"]`, not `["1.0.5"]`)
- `components[].id` in URI format (e.g. `npm://name:ver`)
- `issue_kind != 1` for malicious-package classification

The dropped events do not produce non-2xx responses. The tests are the
only catch.

## Related

- [`docs/storage-kv.md`](./storage-kv.md): KV primitive used by the cursor.
- [`docs/cmd/integration-jfrog-run.md`](./cmd/integration-jfrog-run.md): end-user docs.
- [`docs/ADR.md`](./ADR.md): cross-cutting architectural decisions.
