# JFrog Integration: Developer Guide

Architecture and extension points for `internal/cmd/integration/jfrog/`.
For end-user docs see [`cmd/integration-jfrog-run.md`](./cmd/integration-jfrog-run.md).

## Pieces

| File | Responsibility |
|---|---|
| `cmd.go` | Cobra registration. |
| `run.go` | CLI flag/env resolution → `resolveConfig`. Constructs source + JFrog client and hands to `feedService`. |
| `types.go` | In-memory DTOs (`Config`, `SourceConfig`, `JFrogConfig`). No on-disk schema. |
| `source.go` | `packageSource` interface and `recordHandler` callback type. |
| `source_poll.go` | `pollSource`: gRPC pull implementation backed by a KV cursor. |
| `poller.go` | Single-cycle gRPC pagination + cursor advance. Used only by `pollSource`. |
| `store.go` | `cursorStore`: wraps `*storage.KV[time.Time]`. Used only by `pollSource`. |
| `client.go` | `jfrogClient`: **single source of truth for all JFrog protocol details** (endpoints, authentication, payload format, issue ID rules, version range notation, ecosystem mapping). Owns `Validate`, `PushMaliciousPackage`, `IssueID`. |
| `service.go` | `feedService`: validates via the client, then delegates to the source. |

## Data flow

```
SafeDep                   feedService                JFrog XRay
───────                   ───────────                ──────────
                              │
                              │ 1. client.Validate (GET /policies)
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
       recordHandler ──── client.PushMaliciousPackage ──► POST /xray/api/v1/events
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

Nothing in `feedService` or `jfrogClient` needs to change when a new
source lands.

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

## The `jfrogClient` boundary

All JFrog protocol knowledge lives in `client.go`. If you find yourself
adding any of the following anywhere else in the package, stop and put
it on `jfrogClient` instead:

- A new XRay endpoint or URL path
- HTTP headers JFrog requires (auth, content-type, user-agent)
- A new field in `jfrogEvent` or any other JFrog wire-format struct
- A new ecosystem mapping
- A new vulnerable-version notation
- A new pre-flight check or post-push verification

Why this matters: when JFrog changes the API (new auth header, new
required field, deprecated endpoint), the change is one file. Without
this boundary, JFrog updates produce shotgun edits across the package.

The exposed surface is small on purpose:

```go
type jfrogClient struct{ /* opaque */ }

func newJFrogClient(cfg jfrogConfig) *jfrogClient
func (c *jfrogClient) Validate(ctx context.Context) error
func (c *jfrogClient) PushMaliciousPackage(ctx context.Context, record *...) (issueID string, status int, err error)
func (c *jfrogClient) IssueID(record *...) string
```

`feedService` and `pollSource` see only these methods; they never read
JFrog config fields, never construct URLs, never know that the wire
format is JSON.

## Issue ID format

Every Custom Issue we push to JFrog is keyed by an `id` field. Ours is:

```
"SD-" + record.GetAnalysisId()
```

`AnalysisId` on `ListPackageAnalysisRecordsResponse_AnalysisRecord` is a
ULID (Crockford Base32, 26 chars) issued by the SafeDep backend, e.g.
`01KR0EKN6PMW0ZRFRN992H1PKX`. The full ID looks like:

```
SD-01KR0EKN6PMW0ZRFRN992H1PKX
```

29 chars total, well within JFrog's 32-char `id` limit.

### Why ULID, not name+version

An earlier version of this code built the ID from the package name and
version with truncation budgets, hyphen trimming, and a `-ALL` suffix
for the wildcard version. That had three problems:

1. **Long or scoped names** (`@company/very-long-pkg`) collided with the
   32-char limit, forcing truncation logic that could produce ambiguous
   IDs across versions.
2. **No backend traceability**: the JFrog issue ID gave no way back to
   the SafeDep analysis record that produced it.
3. **Trailing-hyphen surprises** (`money-badger-open-rpc` truncated to
   `money-badger-` produced `SD-MAL-money-badger--199.99.100`).

The ULID-based ID dispenses with all of that. Each analysis record has
exactly one issue ID, regardless of name length or version shape.

### Operator-visible consequence

The "Pushed:" log line shows the package name and version (for human
context); the indented "JFrog:" line shows the actual ID stored in
XRay:

```
✓ Pushed: make-array@0.1.2 (npm)
i   JFrog: SD-01KR0EKN6PMW0ZRFRN992H1PKX [201]
```

A JFrog admin searching for an issue can copy the ULID portion and look
it up in SafeDep directly.

# Flagging All Versions of a Package as Malicious

## @0 version

When a backend sends `package@0` (meaning all versions are malicious), we need to use open ended range in XRay Request.

Using a specific version like `["[1.0.4]"]` only flags that exact version.
Using `["0"]` or `["[0]"]` only flags version `0.0.0`.

We will use the open-ended range notation `(,)` to match all versions:

```json
"components": [
  {
    "id": "veltrix",
    "vulnerable_versions": ["(,)"]
  }
]
```

## Version Range Cheat Sheet

| Use case                    | Format              | Handled |
|-----------------------------|---------------------|---------|
| Specific version            | `["[1.0.4]"]`       | Yes     |
| All versions                | `["(,)"]`           | Yes     |
| From version X onwards      | `["[1.0.0,)"]`      | NO      |
| Up to version X (exclusive) | `["(,2.0.0)"]`      | NO      |
| From X to Y (inclusive)     | `["[1.0.0,2.0.0]"]` | NO      |


Only [1] and [2] are handled since they are the only needed, from our backend also, we will have specific version or all versions (@0, wildcard)

## Malware ID in case of @0

We will use SD-MAL-{pkg-name}-ALL, i.e ALL for version.

## Mapping Rule

When backend sends `package@0` → use `"vulnerable_versions": ["(,)"]`
When backend sends `package@1.0.4` → use `"vulnerable_versions": ["[1.0.4]"]`

## Testing the wire format

`client_test.go` uses `httptest.NewServer` to capture requests and assert
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
