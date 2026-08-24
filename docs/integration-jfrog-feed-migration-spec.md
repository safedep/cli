# Spec: Migrate JFrog integration from malysis polling to ThreatIntel Feed

Status: proposed. Scope: `internal/cmd/integration/jfrog/`.
Companion docs: [integration-jfrog.md](./integration-jfrog.md) (developer guide),
[cmd/integration-jfrog-run.md](./cmd/integration-jfrog-run.md) (end-user).

"This iteration" below means the first delivery of this feed migration (what we build
now, deferring some items to a later pass). It is unrelated to the JFrog XRay API
version, which is also `v1` in the path `/xray/api/v1/events`.

## Goal

`safedep integration jfrog run` ingests verified malicious packages into JFrog XRay
as Custom Issues. Today it pulls from the malysis `ListPackageAnalysisRecords` API.
Move the default data source to the ThreatIntel Feed
(`ThreatIntelService.ListPackageReports`), keeping identical downstream behaviour:
skip suspicious packages, push malicious ones.

The existing poll source stays available behind `--source poll`. Feed is the default.

## Problems this migration solves

### 1. Missed verifications (staleness of the List-API + cursor model)

The current CLI lists malicious packages via the malysis `ListPackageAnalysisRecords`
API and stores a cursor (max `created_at`) in local sqlite. Given how Malysis and Control
Tower (CT) sync today, most listed entries are NOT yet verified at the time they are
listed. When an entry is later verified, Malysis updates it and that update propagates to
CT, but the List-API + `created_at` cursor never re-fetches it: the cursor has already
moved past that record's creation time, and verification does not create a new record. The
CLI only syncs verified entries to JFrog, so it ends up permanently missing a large
fraction of packages that are eventually verified.

The ThreatIntel Feed fixes this at the source. Its cursor is `updated_at`, which moves on
every material change including verification (suspicious to malicious). A report is
re-delivered when it is verified, so the CLI sees the transition and pushes it. Nothing is
missed, because the feed is built around change delivery, not one-shot creation-time listing.

### 2. Undeletable custom issues (no retraction path) - unblocked, not yet enabled

The existing CLI can only POST issues to XRay. It has no way to learn that a package is no
longer considered malicious, so a custom issue, once pushed, is permanent even after a
false positive is retracted. The Feed introduces the `withdrawn` event which, combined with
a reproducible issue id (see "Issue id"), makes deleting the corresponding XRay custom
issue possible for the first time.

This migration lands the groundwork only (reproducible id plus `withdrawn` carried end to
end and counted toward the cursor). The DELETE call itself is the deliberately kept-open
seam (see "Withdrawn handling seam"), targeted for a follow-up rather than this pass.

## Decisions (confirmed)

1. First run (no cursor): backfill last N days, default 7d, configurable via `--backfill`.
2. Suspicious: dropped server-side via `Filters.verdict = THREAT_VERDICT_MALICIOUS`.
3. Withdrawn reports: carried through the pipeline but not acted on yet (logged no-op).
   Code is structured so retraction handling (XRay issue deletion) can be added later
   without a redesign. See "Withdrawn handling seam".
4. Poll source: kept selectable via `--source poll|feed`, default `feed`.
5. Out of scope: indicators/IOCs, campaigns, snapshots.

## Key API differences (malysis List vs ThreatIntel Feed)

| Concern | Old (malysis) | New (threatintel feed) |
|---|---|---|
| Service / method | `MalwareAnalysisService.ListPackageAnalysisRecords` | `ThreatIntelService.ListPackageReports` |
| Transport | gRPC over data-plane conn | same gRPC data-plane conn (no new transport) |
| Record type | `AnalysisRecord` (one pkg@version) | `PackageReport` (one pkg, many versions) |
| Verdict select | `FilterOption{OnlyMalware, OnlyVerified}` | `Filters.verdict = MALICIOUS` |
| Time filter | `start_from` on `created_at`, 7-day cutoff | `filters.since` strict `>` on `updatedAt`, no documented cutoff |
| Sort | implicit | ascending required for reliable incremental sync |
| First run | server default now-1h | full backfill (we override to now minus `backfill`) |
| Cursor watermark | max `created_at` | max `updatedAt` |
| Versions | single string, `"0"` = all | `[]string`, empty = all |
| Issue id source | `AnalysisId` (ULID) | `ReportId` (`SD-<reportId>`), typically 29 chars, guarded at 32 (see "Issue id") |
| Withdrawn | n/a | `withdrawn: true` re-delivery (carried through, no-op for now) |

Note: the Feed is still an incremental pull (paged, cursor-based). "Not polling"
means "no longer the malysis List API", not "no interval loop". The feed source reuses
the same interval-loop shape as the poll source. The endpoint and semantics change.

## Architecture: source-agnostic DTO seam

Today `packageSource`, `recordHandler`, and `jfrogClient` are all typed on
`*malysisv1...AnalysisRecord`. The feed emits `*threatintelv1.PackageReport`. With both
sources coexisting, introduce a neutral internal DTO so `jfrogClient` depends on neither
proto. This aligns with the existing "jfrogClient owns JFrog details, sources own
delivery" design in [integration-jfrog.md](./integration-jfrog.md).

```go
// maliciousPackage is the source-agnostic unit handed to jfrogClient.
// Both sources translate their wire type into this.
type maliciousPackage struct {
    id        string              // stable id for the XRay issue (analysisId or reportId)
    name      string              // component name
    ecosystem packagev1.Ecosystem // reused across both sources
    versions  []string            // empty => all versions affected
    withdrawn bool                // feed retraction; always false from the poll source
}
```

`recordHandler` becomes `func(maliciousPackage) error`.
`jfrogClient` methods take `maliciousPackage`:

- `issueID(mp) string` returns `"SD-" + mp.id`
- `pushMaliciousPackage(ctx, mp) (id string, status int, err error)`
- `buildEvent(mp) (jfrogEvent, bool)`
- (future) `deleteMaliciousPackage(ctx, mp) error` slots in here for withdrawn handling.
  Not built now. See "Withdrawn handling seam".

### Issue id (reproducible, stable, length-guarded)

The XRay custom issue id is the linchpin of both push and future delete, so its
construction must be deterministic and stable. Deletion has no way to look an issue up by
name, so it must reconstruct the exact id that was pushed. This only works if the id is a
pure, reproducible function of the report.

Format: `issueID(mp) = "SD-" + mp.id`, where `mp.id` is the feed `report_id` (or the poll
`analysis_id`). This must be documented for operators in
[integration-jfrog.md](./integration-jfrog.md) so an admin can map an XRay issue back to a
SafeDep report and vice versa.

Reproducibility contract (required for deletion):

- Pure function of the report: no randomness, no timestamps, no truncation, no hashing.
  The same report always yields the same id.
- `report_id` is permanent across the report lifecycle. The schema states verification,
  late indicators, and withdrawal are all updates to the SAME id, never a new report. So a
  `withdrawn` re-delivery carries the exact id originally pushed, and a future delete
  reconstructs `SD-<report_id>` with no stored name-to-id mapping.
- The length guard SKIPS over-length ids, it does not transform them. This keeps push and
  delete symmetric: an id too long to push is also too long to "delete" (it was never
  pushed), so we never try to delete something that does not exist. Truncating or hashing
  would break reproducibility and is therefore forbidden.

Length guard (report_id is not a guaranteed ULID):
The schema comment calls `report_id` "the originating investigation ULID" (26-char
Crockford Base32 in practice, the same id space as the old `AnalysisId`). But the only
machine-enforced constraint is buf.validate `min_len=1, max_len=128`. There is NO ULID
pattern or fixed-length rule. JFrog silently drops any event whose `id` exceeds 32 chars,
and `"SD-" + report_id` could reach 131. So `buildEvent` skips when `len(issueID(mp)) > 32`
with a `drytui.Warning`. The `SD-` prefix also satisfies JFrog's id rules (must not start
with "Xray", not literally "JFrog").

### Skip-rule split (documented change)

Currently all skip rules live in `buildEvent`. With a neutral DTO, source-specific
validity moves into each translator. `buildEvent` keeps only what it can judge from the DTO:

- `buildEvent` skips when `name == ""` (cannot build a component). Empty `versions`
  is now valid (means all versions) and must NOT be skipped.
- Poll translator skips: nil `PackageVersion`, empty name, empty version (unchanged
  behaviour), and maps `"0"` to empty slice (all versions). Always sets `withdrawn=false`.
- Feed translator skips: nil package, empty name. It does NOT drop `withdrawn` reports.
  It sets `mp.withdrawn` and delivers them so the destination layer decides what to do
  (see "Withdrawn handling seam").

[integration-jfrog.md](./integration-jfrog.md) is updated to reflect this split.

## Version mapping

Replace single-value `vulnerableVersions(version string) string` with
`vulnerableVersionRanges(versions []string) []string`:

- `len(versions) == 0` maps to `["(,)"]` (all versions)
- otherwise maps to `["[v1]", "[v2]", ...]` (bracket notation per version)

This lets one report flag multiple malicious versions in a single XRay component,
matching `ReportPackage.versions` semantics. The `"0"` sentinel is handled only in the
poll translator (legacy), never in the feed path.

## Feed source (source_feed.go)

`feedSource` implements `packageSource`, mirroring `pollSource`'s interval loop:

```
subscribe(ctx, onRecord):
  loop until ctx.Done():
    syncOnce(ctx, onRecord)   // one full drain
    sleep(pollInterval)       // reuse existing interval flag
```

`syncOnce` (one drain):

1. Load feed cursor (max `updatedAt`). If zero, `since = now - backfillWindow`.
2. Fix `since` for the whole drain (same "constant per session" rule as poll's
   `start_from`. Only the page token advances).
3. Request per page:
   - `Filters{ since, verdict: MALICIOUS }`
   - `Pagination{ pageSize: 100, sortOrder: ASCENDING, pageToken }`
4. Per report (ascending):
   - translate to `maliciousPackage` (carry `withdrawn`), call `onRecord`.
   - track max `updatedAt` for every report, withdrawn included, so the cursor advances
     past retractions too.
5. After each page, persist cursor = max `updatedAt` seen so far (monotonic, forward-only).
6. Stop when `nextPageToken == ""`. Persist final watermark.

Transient errors (gRPC blip, save failure): log and retry next cycle (unchanged pattern).
`callbackError` still surfaces from subscribe per the recordHandler contract.

### Feed cursor storage

Reuse `cursorStore` and `cursorState{LastSeenAt}` from store.go, but under a separate key
so poll (`created_at`) and feed (`updatedAt`) watermarks never clobber each other when
switching `--source`:

- poll key: `"cursor"` (existing, unchanged)
- feed key: `"feed-cursor"` (new const)

Same namespace `integration-jfrog`, still profile-scoped via `app.ProfileKV`.

No 7-day-cutoff reset logic for the feed (not documented for ThreatIntel). If the API
later rejects a stale `since`, that surfaces as a transient cycle error and is logged.
Adding auto-reset is a follow-up if needed.

### Idempotency / re-delivery

Reports are re-delivered on change (e.g. suspicious to malicious upgrade, new affected
version). XRay push is keyed by `SD-<reportId>`, so a re-push targets the same issue.
This iteration's behaviour: best-effort re-push (POST), log status as today, relying on
XRay upserting on duplicate `id`. The robust fix (existence check + create-or-update) is a
later iteration, see "Planned upsert design".

### Withdrawn handling seam (kept open)

We do not act on retractions yet, but the pipeline is built so we can enable it later
without a redesign:

- `maliciousPackage` carries `withdrawn bool`. The feed translator sets it from
  `PackageReport.GetWithdrawn()`; the poll translator always sets false (malysis has no
  retraction concept).
- Withdrawn reports are delivered (not dropped at the source) and counted toward the
  cursor, so no report is lost when deletion is later turned on.
- `feedService.handleRecord` branches on `mp.withdrawn`. Today the withdrawn branch is a
  logged no-op: `drytui.Info("Withdrawn report %s (%s): retraction handling not yet enabled, skipping", id, name)`.
  It does NOT push. Later it calls `jfrogClient.deleteMaliciousPackage(ctx, mp)`.
- `jfrogClient.do` already accepts any HTTP method, so deletion is a small addition
  (`DELETE <eventsPath>/<issueID>`). The exact JFrog contract is confirmed, see
  "Planned deletion design" below.

Net effect: enabling deletion later touches only `handleRecord`'s withdrawn branch and one
new client method. The source, cursor, DTO shape, and wire-format code are untouched.

### Planned deletion design (post-migration)

NOT built in this migration. Recorded so the follow-up is a small, known change. The
migration deliberately puts every prerequisite in place (reproducible id, `withdrawn`
carried through, cursor advancing past retractions).

JFrog Delete Issue Event API (Custom Issues V1), confirmed from JFrog docs:

- `DELETE /xray/api/v1/events/{id}`, where `{id}` is our `SD-<report_id>`.
- No request body.
- Auth + permission: same as create/push. Reuse the existing `do()` Bearer token. The
  events API is governed by "Manage Xray Metadata", which the token already needs to push.
- Success: `200` with body `{"info":"Vulnerability with id <id> has been successfully deleted"}`.

New client method (only real addition on the client side):

```go
func (c *jfrogClient) deleteMaliciousPackage(ctx context.Context, mp maliciousPackage) (int, error) {
    id := c.issueID(mp)
    if len(id) > 32 {
        // Over-length ids are never pushed (see "Issue id"), so nothing to delete.
        return 0, nil
    }
    status, body, err := c.do(ctx, http.MethodDelete, eventsPath+"/"+id, nil)
    // ... status mapping below
}
```

Response handling (best-effort, mirrors push):

- `200`: deleted. `drytui.Success("Deleted (withdrawn): %s (%s)", name, ecosystem)`,
  `drytui.Info("  JFrog: %s [200]", id)`.
- `404`: issue absent, either already deleted or never pushed (the package was still
  suspicious when last seen, so no issue was created, or its id was over-length). Treat as
  success and log at info. Delete is idempotent from our side.
- other non-2xx / transport error: `drytui.Warning` and continue. A failed delete must not
  stop the feed.

`handleRecord` wiring (replaces the no-op branch):

```go
if mp.withdrawn {
    return s.handleWithdrawn(ctx, mp) // calls client.deleteMaliciousPackage
}
```

Everything else (source, cursor, DTO, id construction, over-length guard) is already in
place from this migration, which is the point of the seam.

### Planned upsert design (2nd iteration, after deletion)

NOT built now. Deferred to a later iteration than deletion. Recorded so the design is ready.

Problem it solves: this migration re-pushes a changed report with a blind POST and relies
on XRay's (unconfirmed) upsert-on-duplicate-id behaviour. A report legitimately changes - a
new malicious version is added, or the verdict/summary is updated - and we want the XRay
issue to reflect that without depending on POST semantics.

JFrog APIs, confirmed from JFrog docs:

- Existence check: `GET /xray/api/v2/events/{id}` (Custom Issues V2). `200` = present,
  `404` = absent. NOTE the version split: existence is a v2 path, while create/update/delete
  are v1 (`/xray/api/v1/events`). Add `eventsPathV2 = "/xray/api/v2/events"` for this.
- Update: `PUT /xray/api/v1/events/{id}` (Custom Issues V1). Full replacement (PUT
  semantics), same body shape as create (`jfrogEvent`), returns `200`. Same Bearer auth and
  "Manage Xray Metadata" permission as push.

Flow (a new `upsertMaliciousPackage`, replacing the blind POST):

1. `GET /api/v2/events/<id>`.
2. `404` -> `POST /api/v1/events` (create, today's path).
3. `200` -> `PUT /api/v1/events/<id>` (update to reflect new versions/fields).

This removes the dependency on POST-upsert behaviour entirely, which is why the open
question below is acceptable to carry in this migration: the proper fix is scoped and known.
Reuses the same reproducible id and over-length guard as push and delete.

## Config and flags (run.go, types.go)

New flags on `run`:

| Flag | Env | Default | Applies |
|---|---|---|---|
| `--source` | `SAFEDEP_INTEGRATION_JFROG_SOURCE` | `feed` | selects `feed` or `poll` |
| `--backfill` | `SAFEDEP_INTEGRATION_JFROG_BACKFILL` | `168h` (7d) | feed first-run window |

Existing flags unchanged: `--instance-url`, `--instance-access-token`, `--poll-interval`.

`sourceConfig` gains `kind` (feed|poll) and `backfillWindow time.Duration`.
`resolveConfig` validates the `--source` value and `--backfill > 0`.

`runCmd` wiring:

- `kind == feed`: build `feedSource` with
  `threatintelv1grpc.NewThreatIntelServiceClient(conn)`, feed cursor KV, pollInterval,
  backfillWindow.
- `kind == poll`: existing `pollSource` wiring (unchanged).

## Files

New:

- `source_feed.go`: feedSource, syncOnce, feed translator.
- `source_feed_test.go`: fake `ThreatIntelServiceClient`, real `*storage.KV` from temp DB
  (mirror poller_test.go). Covers: first-run backfill window, cursor advance, ascending
  paging, malicious-only (verdict filter set), withdrawn delivered and handled as a no-op
  with the cursor still advancing past it, multi-version mapping, all-versions (empty)
  mapping, callbackError surfacing.

Modified:

- `source.go`: `recordHandler` to `func(maliciousPackage) error`, add `maliciousPackage`
  DTO (or new small `package.go`).
- `client.go`: method signatures take `maliciousPackage`, `vulnerableVersions` to
  `vulnerableVersionRanges`, `buildEvent` skip rule = name only.
- `service.go`: `handleRecord` uses DTO fields (`mp.name`, `mp.ecosystem`, joined versions),
  and branches on `mp.withdrawn` (logged no-op for now, the future deletion point).
- `poller.go` / `source_poll.go`: translate `AnalysisRecord` to `maliciousPackage`
  (poll-specific skips plus `"0"` to all), loop unchanged.
- `run.go`: `--source`, `--backfill`, validation, branch wiring.
- `types.go`: `sourceConfig.kind`, `sourceConfig.backfillWindow`.
- `client_test.go`: DTO inputs, assert multi-version and all-versions wire payload,
  over-length issue id (> 32 chars) is skipped, and the id is a pure reproducible function
  of the report (same report yields the same `SD-` id every call).

Docs:

- `docs/integration-jfrog.md`: add feed source row, DTO seam, feed cursor key, skip split,
  and update the "Issue ID format" section for `report_id` plus the id reproducibility and
  delete contract.
- `docs/cmd/integration-jfrog-run.md`: `--source`, `--backfill`, first-run/suspicious/withdrawn.

## Wire payload (unchanged shape, richer versions)

Example: report for `express-logger-pro`, versions `["9.9.9","9.9.10"]`, npm:

```json
{
  "id": "SD-01JZ8Q9V6K3S2M7C1B0A4E5F6G",
  "type": "Security", "provider": "SafeDep", "package_type": "npm",
  "severity": "Critical", "issue_kind": 1,
  "summary": "MALICIOUS PACKAGE: express-logger-pro contains malicious code",
  "components": [{ "id": "express-logger-pro",
                  "vulnerable_versions": ["[9.9.9]", "[9.9.10]"] }],
  "sources": [{ "source_id": "safedep-threat-intel" }]
}
```

All-versions report (empty versions) maps to `"vulnerable_versions": ["(,)"]`.

## Out of scope (this iteration)

IOCs/indicators, campaigns, snapshots bulk download, feed `since` cutoff auto-reset.

## Possible future work (new capabilities, not part of this migration)

- Acting on retractions: deleting a previously pushed XRay issue when its report is later
  withdrawn. This migration does NOT act on withdrawals yet, but deliberately keeps the
  window open (see "Withdrawn handling seam"): the `withdrawn` flag is plumbed end to end
  and counted toward the cursor, so only `handleRecord`'s withdrawn branch plus one
  `deleteMaliciousPackage` client method remain. Issue deletion has never been supported by
  this integration (the poll source has no such path either), so it is a net-new
  capability, not something this migration drops. The XRay delete contract is already
  confirmed and captured in "Planned deletion design (post-migration)", so the follow-up is
  a small, known change.
- Existence-aware upsert of changed reports (GET v2 + create-or-update), replacing the blind
  re-push. Confirmed and captured in "Planned upsert design (2nd iteration, after deletion)".
  Sequenced after deletion.

## Open items to confirm during build (non-blocking)

- XRay upsert semantics on duplicate issue `id` (affects re-delivery and version updates).
  Carried as an accepted risk for this migration only: the robust fix is the 2nd-iteration
  "Planned upsert design" (existence check + create-or-update), which does not depend on it.

Decided (recorded here to avoid re-litigating): the server-side `withdrawn` filter is left
unset so withdrawn reports ARE delivered. This is required to keep the deletion seam viable
later. The `handleRecord` no-op is what suppresses action for now, not a filter.

## Test / verification

- `make lint-conventions`, `make test`.
- Table-driven tests per DEVGUIDE, testify require/assert, fake gRPC client (interface),
  real temp-DB KV. No live network.
