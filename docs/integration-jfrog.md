# JFrog Integration: Developer Guide

Architecture and extension points for `internal/cmd/integration/jfrog/`.
For end-user docs see [`cmd/integration-jfrog-run.md`](./cmd/integration-jfrog-run.md).

The integration reads malicious package reports from the SafeDep ThreatIntel
Feed (`ThreatIntelService.ListPackageReports`) and pushes each one to JFrog XRay
as a Custom Issue. The Feed is the only source.

## Pieces

| File | Responsibility |
|---|---|
| `cmd.go` | Cobra registration. |
| `run.go` | CLI flag/env resolution -> `resolveConfig`. Constructs source + JFrog client and hands to `feedService`. |
| `types.go` | In-memory DTOs (`cmdConfig`, `sourceConfig`, `jfrogConfig`). No on-disk schema. |
| `source.go` | `packageSource` interface and `recordHandler` callback type. |
| `source_feed.go` | `feedSource`: ThreatIntel Feed pull, paging, cursor advance. |
| `store.go` | `cursorStore`: wraps `*storage.KV[cursorState]`. |
| `client.go` | `jfrogClient`: **single source of truth for all JFrog protocol details** (endpoints, authentication, payload format, issue id rules, version range notation, ecosystem mapping). Owns `validate`, `pushMaliciousPackage`, `issueID`. |
| `service.go` | `feedService`: validates via the client, then delegates to the source and routes each report. |

## Data flow

```
SafeDep                   feedService                JFrog XRay
-------                   -----------                ----------
                              |
                              | 1. client.validate (GET /policies)
                              |<----------------------------->
                              |
                              | 2. source.subscribe(ctx, handleRecord)
                              v
                       +--------------+
                       | feedSource   |  ThreatIntelService.ListPackageReports
                       | (gRPC pull)  |  since = max updated_at, ascending
                       +------+-------+
                              | per report
                              v
                        handleRecord
                         |         |
              withdrawn? |         | malicious
                         v         v
   client.deleteMaliciousPackage   client.pushMaliciousPackage
     DELETE /xray/api/v1/events/{id}   POST /xray/api/v1/events
                                       (Bearer token, JSON event)
```

## The `packageSource` contract

```go
type packageSource interface {
    subscribe(ctx context.Context, onRecord recordHandler) error
}

type recordHandler func(*threatintelv1.PackageReport) error
```

Implementations must:

- **Block** until `ctx` is cancelled. The daemon lifecycle is `ctx.Done()`,
  not `subscribe` returning.
- Invoke `onRecord` **exactly once** per report, withdrawn reports included.
- Treat transient errors (gRPC blip, network reset, auth refresh) as
  retryable. Log via `drytui.Warning` and continue. Only return on
  fatal startup errors or context cancellation.
- Own their own resume state. **`feedService` knows nothing about how
  reports are tracked.**

`recordHandler` errors stop further delivery for the current subscribe
session; the source wraps them in `callbackError` internally and surfaces the
unwrapped error from `subscribe`.

## Why this seam

The `packageSource` interface separates *how reports are fetched* (paging, the
`updated_at` cursor, the interval loop) from *what we do with them* (validate
JFrog once, push each report). `feedService` and `jfrogClient` depend only on
the interface and on `*threatintelv1.PackageReport`, never on the feed
transport.

`feedSource` is the source. It replaced the earlier malysis
`ListPackageAnalysisRecords` poll source, which is gone. If SafeDep later
exposes a different transport for the same reports, it implements
`packageSource` and nothing in `feedService` or `jfrogClient` changes.

## Adding a new source

1. Create `source_<name>.go` in this package.
2. Define a struct that implements `packageSource`.
3. The constructor takes whatever transport state is needed. Keep it
   private. `run.go` does the wiring.
4. In `subscribe`, drive your delivery loop until `ctx.Done()`. Call
   `onRecord` per report. Do not return on transient errors. Wrap a
   handler error in `callbackError` so it surfaces instead of retrying.
5. Add tests using a fake transport (see `source_feed_test.go` for the
   pattern with a fake gRPC client and a real `*storage.KV` from a
   temp DB).

## Cursor semantics

`feedSource` uses a profile-scoped KV under namespace `"integration-jfrog"`
with a single key `"cursor"`. The value is `cursorState{LastSeenAt}`, the max
`updated_at` seen so far.

| Cursor state | Behaviour |
|---|---|
| Missing key (`storage.ErrNotFound`) | First run. `since = now - backfill`; default backfill `0` means `since = now` (fresh). |
| Decode failure (`storage.ErrKVDecode`) | Stale schema. Delete + warn + start fresh. |
| DB error (locked, permission) | Propagate. The next cycle retries; never silently destroy. |
| In-window | Used as `since`. **Constant for the entire drain.** |

Rules that matter:

- **`since` is a strict `>` filter on `updated_at`.** The Feed re-delivers a
  report on every material change (a suspicious to malicious upgrade, a new
  affected version, a withdrawal), so an `updated_at` cursor never moves past a
  report before it is verified. This is the whole reason for the migration off
  the creation-time list API.
- **`since` is never omitted.** An omitted filter would pull the full feed
  history. A fresh start is `since = now`, not an absent filter.
- **`since` is fixed for the whole drain.** Only `next_page_token` moves
  forward within a drain. Reports are requested in **ascending `updated_at`**
  order so an interrupted drain resumes without gaps.
- **The cursor is forward-only.** It advances to the max `updated_at` of the
  page, withdrawn reports included, so retractions are not re-delivered forever.
- **No 7-day cutoff.** The Feed does not document a `since` cutoff, so there is
  no stale-cursor reset. If the API later rejects a stale `since`, that surfaces
  as a transient cycle error and is logged; auto-reset is a follow-up if needed.

Upgrade note: a deployment upgrading from the old poll build may have a
`"cursor"` value that was a `created_at` watermark. It is still a timestamp and
remains usable as `since` (at worst it re-delivers a little).

## Logging boundary

Per [AGENTS.md](../AGENTS.md):

| Library | Use for | Examples in this package |
|---|---|---|
| `drytui` (`Info`, `Success`, `Warning`) | Operator-visible messages. State changes, errors the user can act on. | `Pushed: ...`, `Deleted: ...`, `Push failed for X`, `Delete failed for X`, `Skipping report: missing package name` |
| `dry/log` (`Warnf`, etc.) | Internal diagnostics. Not actionable. | Deferred body close failure, bounded body read failure |

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

When JFrog changes the API, the change is one file.

The exposed surface is small on purpose:

```go
type jfrogClient struct{ /* opaque */ }

func newJFrogClient(cfg jfrogConfig) *jfrogClient
func (c *jfrogClient) validate(ctx context.Context) error
func (c *jfrogClient) pushMaliciousPackage(ctx context.Context, report *threatintelv1.PackageReport) (issueID string, status int, err error)
func (c *jfrogClient) issueID(report *threatintelv1.PackageReport) string
```

`feedService` and `feedSource` see only these methods; they never read JFrog
config fields, never construct URLs, never know that the wire format is JSON.

There is **no neutral DTO**. With a single wire type, a translation layer would
be dead weight, and `jfrogClient` is coupled to a SafeDep proto either way.

## Issue ID format

Every Custom Issue we push is keyed by an `id` field. Ours is:

```
"SD-" + report.GetReportId()
```

for example `SD-01KR0EKN6PMW0ZRFRN992H1PKX`. A JFrog admin can copy the portion
after `SD-` and look the report up in SafeDep, and vice versa.

### Reproducibility contract

`issueID` is a **pure, reproducible function of `report_id`**: no randomness, no
timestamps, no truncation, no hashing. The same report always yields the same
id. This is a hard requirement for later stages:

- Delete and update (Stage 3) have no way to look an issue up by name. They
  reconstruct the exact id that was pushed.
- `report_id` is permanent across the report lifecycle. Verification, late
  indicators, and withdrawal are all updates to the **same** id, never a new
  report. So a withdrawn or updated re-delivery carries the id originally
  pushed, and delete/update reconstruct `SD-<report_id>` with no stored
  name-to-id mapping.

### Length guard

`report_id` is **not** a guaranteed 26-char ULID. The schema comment calls it
"the originating investigation ULID" (26-char Crockford Base32 in practice), but
the only machine-enforced constraint is `min_len=1, max_len=128`. JFrog silently
drops any event whose `id` exceeds 32 chars, and `"SD-" + report_id` could reach
131.

So `buildEvent` **skips** a report when `len(issueID(report)) > 32`, with a
`drytui.Warning`. The guard skips, it does not transform: a truncated or hashed
id would break the reproducibility contract, and an id too long to push is also
too long to delete or update (it was never pushed). This keeps push, delete, and
update symmetric.

# Flagging package versions as malicious

A report's `ReportPackage.versions` may be empty (every version is affected) or
list one or more exact versions. All of them map into **one component's**
`vulnerable_versions` (one report = one issue = one component, regardless of
version count).

`vulnerableVersionRanges(versions []string) []string`:

- Bracket every non-empty entry: `"1.0.0"` -> `"[1.0.0]"`.
- Drop empty-string entries. The feed bounds each version to `<= 256` chars but
  has no min length, and `"[]"` is silently dropped by XRay, so an empty entry
  must not slip into an otherwise valid list.
- If nothing remains (input empty, or every entry empty) return `["(,)"]` (all
  versions).

Examples:

| `versions` | `vulnerable_versions` |
|---|---|
| `[]` | `["(,)"]` (all versions) |
| `["1.0.0"]` | `["[1.0.0]"]` |
| `["1.0.0","1.0.1","2.0.0"]` | `["[1.0.0]","[1.0.1]","[2.0.0]"]` |

## Version Range Cheat Sheet

| Use case | Format | Handled |
|---|---|---|
| Specific version | `["[1.0.4]"]` | Yes |
| All versions | `["(,)"]` | Yes |
| From version X onwards | `["[1.0.0,)"]` | No |
| Up to version X (exclusive) | `["(,2.0.0)"]` | No |
| From X to Y (inclusive) | `["[1.0.0,2.0.0]"]` | No |

Only exact versions and all-versions are produced, because that is all the feed
reports: a set of exact affected versions, or an empty set meaning all.

## Withdrawn reports

The feed re-delivers a report with `withdrawn = true` when a package is no
longer considered malicious (for example a false positive is retracted).

- The **source** does not drop withdrawn reports. It delivers them and advances
  the cursor past them.
- **`feedService.handleRecord`** branches on `report.GetWithdrawn()`. A withdrawn
  report is deleted with `client.deleteMaliciousPackage`, a `DELETE` of the XRay
  issue keyed by the reproducible `SD-<report_id>`. A 404 is benign (the issue is
  already gone), and an over-length id is skipped (it was never pushed).

## Issue summary and description

The XRay Custom Issue carries a `summary` (short headline) and a `description`
(longer body):

- `summary` is always the synthesized headline
  `MALICIOUS PACKAGE: <name> contains malicious code`. The feed `title` is not
  used: it is only the first few words of the feed summary, so it makes a poor
  standalone headline.
- `description` <- `report.GetSummary()` (the feed's full summary). When the feed
  summary is empty, `buildEvent` falls back to
  `<name> has been identified as a malicious package by SafeDep threat intelligence.`
  so the field is never blank.

## Testing the wire format

`client_test.go` uses `httptest.NewServer` to capture requests and assert
the payload byte-for-byte against the JFrog reference. **Do not skip these
tests when changing the payload.** JFrog silently drops events that violate any
of:

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
