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
Move to the ThreatIntel Feed (`ThreatIntelService.ListPackageReports`), keeping identical
downstream behaviour: skip suspicious packages, push malicious ones.

The Feed becomes the only source. The malysis poll source is removed.

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

1. Source: ThreatIntel Feed only. The malysis poll source is removed (no `--source` flag).
2. First run (no cursor): start fresh from now by default (`--backfill 0`). Extendable via
   `--backfill` (e.g. `7d`), which seeds `since = now - backfill`.
3. Suspicious: dropped server-side via `Filters.verdict = THREAT_VERDICT_MALICIOUS`.
4. Withdrawn reports: carried through the pipeline but not acted on yet (logged no-op).
   Code is structured so retraction handling (XRay issue deletion) can be added later
   without a redesign. See "Withdrawn handling seam".
5. Out of scope: indicators/IOCs, campaigns, snapshots.

## Key API differences (malysis List vs ThreatIntel Feed)

| Concern | Old (malysis, removed) | New (threatintel feed) |
|---|---|---|
| Service / method | `MalwareAnalysisService.ListPackageAnalysisRecords` | `ThreatIntelService.ListPackageReports` |
| Transport | gRPC over data-plane conn | same gRPC data-plane conn (no new transport) |
| Record type | `AnalysisRecord` (one pkg@version) | `PackageReport` (one pkg, many versions) |
| Verdict select | `FilterOption{OnlyMalware, OnlyVerified}` | `Filters.verdict = MALICIOUS` |
| Time filter | `start_from` on `created_at`, 7-day cutoff | `filters.since` strict `>` on `updatedAt`, no documented cutoff |
| Sort | implicit | ascending required for reliable incremental sync |
| First run | server default now-1h | `since = now - backfill`, default backfill 0 (fresh from now) |
| Cursor watermark | max `created_at` | max `updatedAt` |
| Versions | single string, `"0"` = all | `[]string`, empty = all |
| Issue id source | `AnalysisId` (ULID) | `ReportId` (`SD-<reportId>`), typically 29 chars, guarded at 32 (see "Issue id") |
| Withdrawn | n/a | `withdrawn: true` re-delivery (carried through, no-op for now) |

Note: the Feed is still an incremental pull (paged, cursor-based). Removing "polling" means
removing the malysis List API, not the interval loop. The feed source keeps an interval
loop (sleep between drains); the `--poll-interval` flag is retained and now paces feed
cycles.

## Architecture (retype to the feed proto, remove poll)

Keep the existing seams described in [integration-jfrog.md](./integration-jfrog.md):
`feedService` validates JFrog once, then delegates to a `packageSource`; `jfrogClient` owns
all JFrog protocol details. The migration:

- Removes the poll source (`poller.go`, `source_poll.go`, `poller_test.go`) and the malysis
  dependency from this package.
- Adds `feedSource` implementing `packageSource`.
- Retypes `recordHandler` and the `jfrogClient` methods from
  `*malysisv1...AnalysisRecord` to `*threatintelv1.PackageReport`.

No neutral DTO. With a single source, a translation layer would be dead weight, and
`jfrogClient` is already coupled to a SafeDep proto today - this swaps which one. Any future
SafeDep source (e.g. a `streamSource`) also yields `PackageReport`, so the `packageSource`
seam still generalises.

`jfrogClient` surface (retyped):

- `issueID(report) string` returns `"SD-" + report.GetReportId()`
- `pushMaliciousPackage(ctx, report) (id string, status int, err error)`
- `buildEvent(report) (jfrogEvent, bool)`
- (future) `deleteMaliciousPackage(ctx, report) error` slots in here. See "Withdrawn
  handling seam".

`recordHandler` becomes `func(*threatintelv1.PackageReport) error`. `callbackError`
(source.go) is unchanged and still distinguishes handler errors from transient infra errors.

### Issue id (reproducible, stable, length-guarded)

The XRay custom issue id is the linchpin of both push and future delete, so its
construction must be deterministic and stable. Deletion has no way to look an issue up by
name, so it must reconstruct the exact id that was pushed. This only works if the id is a
pure, reproducible function of the report.

Format: `issueID(report) = "SD-" + report.GetReportId()`. This must be documented for
operators in [integration-jfrog.md](./integration-jfrog.md) so an admin can map an XRay
issue back to a SafeDep report and vice versa.

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
and `"SD-" + report_id` could reach 131. So `buildEvent` skips when `len(issueID(report)) > 32`
with a `drytui.Warning`. The `SD-` prefix also satisfies JFrog's id rules (must not start
with "Xray", not literally "JFrog").

### Skip rules

With a single source, skip rules stay in `buildEvent` (co-located with the wire format, as
today):

- Skip when the package is nil or `name == ""` (cannot build a component), with a
  `drytui.Warning`.
- Empty `versions` is valid (means all versions) and is NOT skipped.
- Withdrawn reports are NOT skipped in `buildEvent`. The source delivers them and
  `feedService.handleRecord` decides (no-op now). See "Withdrawn handling seam".

## Version mapping

`vulnerableVersionRanges(versions []string) []string`:

- `len(versions) == 0` maps to `["(,)"]` (all versions)
- otherwise maps to `["[v1]", "[v2]", ...]` (bracket notation per version)

One report can flag multiple malicious versions in a single XRay component, matching
`ReportPackage.versions`. There is no `"0"` sentinel: the feed signals all-versions with an
empty slice.

## Feed source (source_feed.go)

`feedSource` implements `packageSource` with an interval loop:

```
subscribe(ctx, onRecord):
  loop until ctx.Done():
    syncOnce(ctx, onRecord)   // one full drain
    sleep(pollInterval)       // paces feed cycles (--poll-interval)
```

`syncOnce` (one drain):

1. Load cursor (max `updatedAt`). If none, `since = now - backfillWindow`
   (default backfill 0 -> `since = now`, i.e. fresh).
2. Fix `since` for the whole drain (constant per session; only the page token advances).
3. Request per page:
   - `Filters{ since, verdict: MALICIOUS }`
   - `Pagination{ pageSize: 100, sortOrder: ASCENDING, pageToken }`
4. Per report (ascending):
   - call `onRecord(report)` (which carries `withdrawn` downstream).
   - track max `updatedAt` for every report, withdrawn included, so the cursor advances
     past retractions too.
5. After each page, persist cursor = max `updatedAt` seen so far (monotonic, forward-only).
6. Stop when `nextPageToken == ""`. Persist final watermark.

Transient errors (gRPC blip, save failure): log and retry next cycle. `callbackError`
surfaces from subscribe per the recordHandler contract.

Never omit `since`. Omitting it would request the full feed history; a fresh start is
`since = now` (backfill 0), not an omitted filter.

### Cursor storage

Single profile-scoped KV (via `app.ProfileKV`), namespace `integration-jfrog`, key
`"cursor"`, value `cursorState{LastSeenAt}` = max `updatedAt`. `store.go` (`cursorStore`,
`cursorState`) is reused unchanged. Poll is gone, so there is no second key.

Upgrade note: a deployment upgrading from the poll build may have a `"cursor"` value that
was a `created_at` watermark. It is still a timestamp and remains usable as `since`
(harmless, at worst re-delivers a little). Fresh installs have no cursor -> `since = now - backfill`.

No 7-day-cutoff reset logic (not documented for ThreatIntel). If the API later rejects a
stale `since`, that surfaces as a transient cycle error and is logged; auto-reset is a
follow-up if needed.

### Idempotency / re-delivery

Reports are re-delivered on change (e.g. suspicious to malicious upgrade, new affected
version). XRay push is keyed by `SD-<reportId>`, so a re-push targets the same issue.
This iteration's behaviour: best-effort re-push (POST), log status as today, relying on
XRay upserting on duplicate `id`. The robust fix (existence check + create-or-update) is a
later iteration, see "Planned upsert design".

### Withdrawn handling seam (kept open)

We do not act on retractions yet, but the pipeline is built so we can enable it later
without a redesign:

- Withdrawn reports are delivered (not dropped at the source) and counted toward the
  cursor, so no report is lost when deletion is later turned on.
- `feedService.handleRecord` branches on `report.GetWithdrawn()`. Today the withdrawn
  branch is a logged no-op:
  `drytui.Info("Withdrawn report %s (%s): retraction handling not yet enabled, skipping", id, name)`.
  It does NOT push. Later it calls `jfrogClient.deleteMaliciousPackage(ctx, report)`.
- `jfrogClient.do` already accepts any HTTP method, so deletion is a small addition
  (`DELETE <eventsPath>/<issueID>`). The exact JFrog contract is confirmed, see
  "Planned deletion design" below.

Net effect: enabling deletion later touches only `handleRecord`'s withdrawn branch and one
new client method. The source, cursor, and wire-format code are untouched.

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
func (c *jfrogClient) deleteMaliciousPackage(ctx context.Context, report *threatintelv1.PackageReport) (int, error) {
    id := c.issueID(report)
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
if report.GetWithdrawn() {
    return s.handleWithdrawn(ctx, report) // calls client.deleteMaliciousPackage
}
```

Everything else (source, cursor, id construction, over-length guard) is already in place
from this migration, which is the point of the seam.

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

New flag on `run`:

| Flag | Env | Default | Meaning |
|---|---|---|---|
| `--backfill` | `SAFEDEP_INTEGRATION_JFROG_BACKFILL` | `0` | first-run window; `0` = fresh from now |

Existing flags unchanged: `--instance-url`, `--instance-access-token`, `--poll-interval`.
The `--source` flag is not added (feed is the only source).

`sourceConfig` drops any source-kind field and gains `backfillWindow time.Duration`.
`resolveConfig` validates `--backfill >= 0` (0 is valid; negative is rejected).

`runCmd` wiring: build `feedSource` with
`threatintelv1grpc.NewThreatIntelServiceClient(conn)`, the cursor KV, `pollInterval`, and
`backfillWindow`; hand it to `feedService` which validates JFrog then runs.

## Files

New:

- `source_feed.go`: feedSource, syncOnce.
- `source_feed_test.go`: fake `ThreatIntelServiceClient`, real `*storage.KV` from temp DB.
  Covers: fresh first run (backfill 0 -> `since = now`), non-zero backfill window, cursor
  advance, ascending paging, malicious-only (verdict filter set), withdrawn delivered and
  handled as a no-op with the cursor still advancing past it, multi-version mapping,
  all-versions (empty) mapping, callbackError surfacing.

Removed:

- `poller.go`, `source_poll.go`, `poller_test.go`: the malysis poll source. Also drop
  malysis imports elsewhere in the package.

Modified:

- `source.go`: `recordHandler` to `func(*threatintelv1.PackageReport) error`. `callbackError`
  unchanged.
- `client.go`: methods take `*threatintelv1.PackageReport`; `vulnerableVersions` to
  `vulnerableVersionRanges`; `buildEvent` skips nil package / empty name; add over-length id
  guard; `issueID` uses `GetReportId()`.
- `service.go`: `handleRecord` reads report fields and branches on `report.GetWithdrawn()`
  (logged no-op for now, the future deletion point).
- `run.go`: drop `--source`; add `--backfill` (default 0); feed-only wiring.
- `types.go`: drop source-kind; add `sourceConfig.backfillWindow`.
- `cmd.go`: help text touch-up if needed.
- `client_test.go`: report inputs; assert multi-version and all-versions wire payload,
  over-length issue id (> 32 chars) is skipped, and the id is a pure reproducible function
  of the report (same report yields the same `SD-` id every call).
- `store.go`: reused unchanged.

Docs:

- `docs/integration-jfrog.md`: replace the poll/pollSource content with the feed source,
  update the cursor semantics (updatedAt, single key), the DTO-free client boundary, and the
  "Issue ID format" section for `report_id` plus the id reproducibility and delete contract.
- `docs/cmd/integration-jfrog-run.md`: `--backfill` (default 0, fresh), remove `--source`,
  first-run/suspicious/withdrawn behaviour.

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
  `deleteMaliciousPackage` client method remain. The XRay delete contract is already
  confirmed and captured in "Planned deletion design (post-migration)", so the follow-up is
  a small, known change.
- Existence-aware upsert of changed reports (GET v2 + create-or-update), replacing the blind
  re-push. Confirmed and captured in "Planned upsert design (2nd iteration, after deletion)".
  Sequenced after deletion.

## Open items to confirm during build (non-blocking)

- XRay upsert semantics on duplicate issue `id` (affects re-delivery and version updates).
  Carried as an accepted risk for this migration only: the robust fix is the 2nd-iteration
  "Planned upsert design" (existence check + create-or-update), which does not depend on it.

## Test / verification

- `make lint-conventions`, `make test`.
- Table-driven tests per DEVGUIDE, testify require/assert, fake gRPC client (interface),
  real temp-DB KV. No live network.
