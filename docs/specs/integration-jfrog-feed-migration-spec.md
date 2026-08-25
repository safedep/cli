# Spec: Migrate JFrog integration from malysis polling to ThreatIntel Feed

Status: proposed. Scope: `internal/cmd/integration/jfrog/`.
Companion docs: [integration-jfrog.md](../integration-jfrog.md) (developer guide),
[cmd/integration-jfrog-run.md](../cmd/integration-jfrog-run.md) (end-user).

Delivered in three stages (see "Staged delivery"). "Stage N" is our delivery phase and is
unrelated to the JFrog XRay API version, which appears as `v1`/`v2` in paths like
`/xray/api/v1/events`.

## Goal

`safedep integration jfrog run` ingests verified malicious packages into JFrog XRay
as Custom Issues. Today it pulls from the malysis `ListPackageAnalysisRecords` API.
Move to the ThreatIntel Feed (`ThreatIntelService.ListPackageReports`): skip suspicious
packages, push malicious ones, and eventually delete/update issues as reports change.

The Feed becomes the only source. The malysis poll source is removed.

## Problems this migration solves

### 1. Missed verifications (staleness of the List-API + cursor model)  [Stage 1]

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

### 2. Undeletable custom issues (no retraction path)  [Stage 2]

The existing CLI can only POST issues to XRay. It has no way to learn that a package is no
longer considered malicious, so a custom issue, once pushed, is permanent even after a
false positive is retracted. The Feed introduces the `withdrawn` event which, combined with
a reproducible issue id (see "Issue id"), lets us delete the corresponding XRay custom
issue. Stage 1 lays the groundwork (reproducible id, `withdrawn` carried end to end and
counted toward the cursor); Stage 2 performs the DELETE.

## Staged delivery

Each stage is an independent, shippable change. Stage 1 puts every prerequisite in place so
Stages 2 and 3 are small, localized additions.

| Stage | Scope | JFrog API | Solves |
|---|---|---|---|
| 1 | Pure migration: feed-only source, push malicious, skip suspicious. Withdrawn carried through but a logged no-op. | `POST /api/v1/events`, `GET /api/v1/policies` | Problem 1 |
| 2 | Withdrawn handling: delete the XRay issue on retraction. | `DELETE /api/v1/events/{id}` | Problem 2 |
| 3 | Existence-aware upsert: update a changed issue instead of blind re-push. | `GET /api/v2/events/{id}` then `POST`/`PUT /api/v1/events/{id}` | Re-delivery correctness |

The rest of this spec designs Stage 1 in full (it is what we build first) and records the
confirmed contracts for Stages 2 and 3 so they are ready.

## Decisions (confirmed)

1. Source: ThreatIntel Feed only. The malysis poll source is removed (no `--source` flag).
2. First run (no cursor): start fresh from now by default (`--backfill 0`). Extendable via
   `--backfill` (e.g. `7d`), which seeds `since = now - backfill`.
3. Suspicious: dropped server-side via `Filters.verdict = THREAT_VERDICT_MALICIOUS`.
4. Withdrawn reports: Stage 1 carries them through the pipeline as a logged no-op; Stage 2
   deletes the XRay issue. See "Stage 2 design".
5. Update of a changed issue (PUT / existence-aware upsert): Stage 3. See "Stage 3 design".
6. Out of scope entirely: indicators/IOCs, campaigns, snapshots.

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
| Withdrawn | n/a | `withdrawn: true` re-delivery (Stage 1 no-op, Stage 2 deletes) |

Note: the Feed is still an incremental pull (paged, cursor-based). Removing "polling" means
removing the malysis List API, not the interval loop. The feed source keeps an interval
loop (sleep between drains); the `--poll-interval` flag is retained and now paces feed
cycles.

## Architecture (Stage 1: retype to the feed proto, remove poll)

Keep the existing seams described in [integration-jfrog.md](../integration-jfrog.md):
`feedService` validates JFrog once, then delegates to a `packageSource`; `jfrogClient` owns
all JFrog protocol details. Stage 1:

- Removes the poll source (`poller.go`, `source_poll.go`, `poller_test.go`) and the malysis
  dependency from this package.
- Adds `feedSource` implementing `packageSource`.
- Retypes `recordHandler` and the `jfrogClient` methods from
  `*malysisv1...AnalysisRecord` to `*threatintelv1.PackageReport`.

No neutral DTO. With a single source, a translation layer would be dead weight, and
`jfrogClient` is already coupled to a SafeDep proto today - this swaps which one. Any future
SafeDep source (e.g. a `streamSource`) also yields `PackageReport`, so the `packageSource`
seam still generalises.

`jfrogClient` surface after Stage 1:

- `issueID(report) string` returns `"SD-" + report.GetReportId()`
- `pushMaliciousPackage(ctx, report) (id string, status int, err error)`
- `buildEvent(report) (jfrogEvent, bool)`
- (Stage 2) `deleteMaliciousPackage(ctx, report) (status int, err error)`
- (Stage 3) existence check + `PUT` update

`recordHandler` becomes `func(*threatintelv1.PackageReport) error`. `callbackError`
(source.go) is unchanged and still distinguishes handler errors from transient infra errors.

### Issue id (reproducible, stable, length-guarded)

The XRay custom issue id is the linchpin of push (Stage 1), delete (Stage 2), and update
(Stage 3), so its construction must be deterministic and stable. Delete/update have no way
to look an issue up by name; they must reconstruct the exact id that was pushed. This only
works if the id is a pure, reproducible function of the report.

Format: `issueID(report) = "SD-" + report.GetReportId()`. This must be documented for
operators in [integration-jfrog.md](../integration-jfrog.md) so an admin can map an XRay
issue back to a SafeDep report and vice versa.

Reproducibility contract (required for Stages 2 and 3):

- Pure function of the report: no randomness, no timestamps, no truncation, no hashing.
  The same report always yields the same id.
- `report_id` is permanent across the report lifecycle. The schema states verification,
  late indicators, and withdrawal are all updates to the SAME id, never a new report. So a
  `withdrawn` (or updated) re-delivery carries the exact id originally pushed, and a later
  delete/update reconstructs `SD-<report_id>` with no stored name-to-id mapping.
- The length guard SKIPS over-length ids, it does not transform them. This keeps push,
  delete, and update symmetric: an id too long to push is also too long to delete/update
  (it was never pushed). Truncating or hashing would break reproducibility and is forbidden.

Length guard (report_id is not a guaranteed ULID):
The schema comment calls `report_id` "the originating investigation ULID" (26-char
Crockford Base32 in practice, the same id space as the old `AnalysisId`). But the only
machine-enforced constraint is buf.validate `min_len=1, max_len=128`. There is NO ULID
pattern or fixed-length rule. JFrog silently drops any event whose `id` exceeds 32 chars,
and `"SD-" + report_id` could reach 131. So `buildEvent` skips when `len(issueID(report)) > 32`
with a `drytui.Warning`. The `SD-` prefix also satisfies JFrog's id rules (must not start
with "Xray", not literally "JFrog").

### Skip rules

With a single source, skip rules stay in `buildEvent` (co-located with the wire format):

- Skip when the package is nil or `name == ""` (cannot build a component), with a
  `drytui.Warning`.
- Empty `versions` is valid (means all versions) and is NOT skipped.
- Withdrawn reports are NOT skipped in `buildEvent`. The source delivers them and
  `feedService.handleRecord` decides (Stage 1: no-op; Stage 2: delete).

## Version mapping

A report's `ReportPackage.versions` may be empty (all versions) or list one or more exact
versions. All of them map into a single component's `vulnerable_versions` (one report = one
issue = one component, regardless of version count).

`vulnerableVersionRanges(versions []string) []string`:

- Bracket every non-empty entry: `"1.0.0"` -> `"[1.0.0]"`.
- Drop empty-string entries. The feed's per-item validate rule bounds each version to
  <= 256 chars but has NO min length, so an empty entry is possible, and `"[]"` is silently
  dropped by XRay. Skipping it avoids a malformed range slipping into an otherwise valid list.
- If nothing remains (input was empty, or every entry was empty) return `["(,)"]`
  (all versions).

```go
func vulnerableVersionRanges(versions []string) []string {
    ranges := make([]string, 0, len(versions))
    for _, v := range versions {
        if v == "" {
            continue // "[]" is silently dropped by XRay
        }
        ranges = append(ranges, "["+v+"]")
    }
    if len(ranges) == 0 {
        return []string{"(,)"} // empty list => every version affected
    }
    return ranges
}
```

Examples:

- `[]` -> `["(,)"]` (all versions)
- `["1.0.0"]` -> `["[1.0.0]"]`
- `["1.0.0","1.0.1","2.0.0"]` -> `["[1.0.0]","[1.0.1]","[2.0.0]"]` (one component)

There is no `"0"` sentinel: the feed signals all-versions with an empty slice.

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
Stage 1 behaviour: best-effort re-push (POST), log status as today, relying on XRay
upserting on duplicate `id`. The robust fix (existence check + create-or-update) is Stage 3.

## Stage 1: withdrawn is a no-op (seam kept open for Stage 2)

Stage 1 delivers withdrawn reports (does not drop them at the source) and counts them toward
the cursor, so nothing is lost when Stage 2 turns on deletion. `feedService.handleRecord`
branches on `report.GetWithdrawn()`:

```go
if report.GetWithdrawn() {
    // Stage 1: logged no-op. Stage 2 replaces this with a delete.
    drytui.Info("Withdrawn report %s (%s): retraction handling not yet enabled, skipping", id, name)
    return nil
}
return s.handlePush(ctx, report)
```

Net effect: Stage 2 replaces only this branch (plus one client method). The source, cursor,
id construction, over-length guard, and wire-format code are untouched.

## Stage 2 design (withdrawn -> delete)

NOT built in Stage 1. Contract confirmed from JFrog docs; recorded so Stage 2 is a small,
known change.

JFrog Delete Issue Event API (Custom Issues V1):

- `DELETE /xray/api/v1/events/{id}`, where `{id}` is our `SD-<report_id>`.
- No request body.
- Auth + permission: same as create/push. Reuse the existing `do()` Bearer token. The
  events API is governed by "Manage Xray Metadata", which the token already needs to push.
- Success: `200` with body `{"info":"Vulnerability with id <id> has been successfully deleted"}`.

Delivery dependency (verify at Stage 2 build time): we only ever pushed MALICIOUS reports,
so to delete them we must receive their `withdrawn` events. With the server-side
`verdict = MALICIOUS` filter this works ONLY IF a withdrawn report retains
`verdict = MALICIOUS`. The docs indicate withdrawal just sets `withdrawn: true` (verdict not
downgraded), so this should hold. Fallback if it does not: drop the server-side verdict
filter and classify verdict client-side, so withdrawn events arrive regardless of verdict.

Client method:

```go
func (c *jfrogClient) deleteMaliciousPackage(ctx context.Context, report *threatintelv1.PackageReport) (int, error) {
    id := c.issueID(report)
    if len(id) > 32 {
        return 0, nil // never pushed (see "Issue id"), nothing to delete
    }
    status, body, err := c.do(ctx, http.MethodDelete, eventsPath+"/"+id, nil)
    // ... status mapping below
}
```

Response handling (best-effort, mirrors push):

- `200`: deleted. `drytui.Success("Deleted (withdrawn): %s (%s)", name, ecosystem)`.
- `404`: issue absent, either already deleted or never pushed (still suspicious when last
  seen, or over-length id). Treat as success and log at info. Idempotent from our side.
- other non-2xx / transport error: `drytui.Warning` and continue; a failed delete must not
  stop the feed.

`handleRecord` wiring replaces the Stage 1 no-op branch with a call to
`handleWithdrawn` -> `deleteMaliciousPackage`.

## Stage 3 design (existence-aware upsert)

NOT built in Stage 1 or 2. Contract confirmed; recorded so Stage 3 is ready.

Problem it solves: Stages 1-2 re-push a changed report with a blind POST and rely on XRay's
(unconfirmed) upsert-on-duplicate-id behaviour. A report legitimately changes - a new
malicious version is added, or the verdict/summary is updated - and we want the XRay issue
to reflect that without depending on POST semantics.

JFrog APIs:

- Existence check: `GET /xray/api/v2/events/{id}` (Custom Issues V2). `200` = present,
  `404` = absent. NOTE the version split: existence is a v2 path, while create/update/delete
  are v1 (`/xray/api/v1/events`). Add `eventsPathV2 = "/xray/api/v2/events"` for this.
- Update: `PUT /xray/api/v1/events/{id}` (Custom Issues V1). Full replacement (PUT
  semantics), same body shape as create (`jfrogEvent`), returns `200`. Same Bearer auth and
  "Manage Xray Metadata" permission as push.

Flow (a new `upsertMaliciousPackage`, replacing the blind POST):

1. `GET /api/v2/events/<id>`.
2. `404` -> `POST /api/v1/events` (create, Stage 1 path).
3. `200` -> `PUT /api/v1/events/<id>` (update to reflect new versions/fields).

This removes the dependency on POST-upsert behaviour entirely, which is why the open
question below is acceptable to carry through Stages 1-2: the proper fix is scoped and known.
Reuses the same reproducible id and over-length guard as push and delete.

## Config and flags (run.go, types.go)

New flag on `run` (Stage 1):

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

## Files (Stage 1)

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
- `service.go`: `handleRecord` branches on `report.GetWithdrawn()` (Stage 1: logged no-op)
  and otherwise pushes.
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
  "Issue ID format" section for `report_id` plus the id reproducibility contract.
- `docs/cmd/integration-jfrog-run.md`: `--backfill` (default 0, fresh), remove `--source`,
  first-run/suspicious/withdrawn behaviour.

Stages 2 and 3 add: `deleteMaliciousPackage` / `upsertMaliciousPackage` on the client, the
`handleWithdrawn` branch, `eventsPathV2`, and their tests. No Stage 1 rework.

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

## Out of scope (all stages)

IOCs/indicators, campaigns, snapshots bulk download, feed `since` cutoff auto-reset.

## Open items to confirm during build (non-blocking for Stage 1)

- XRay upsert semantics on duplicate issue `id` (affects re-delivery and version updates).
  Accepted risk through Stages 1-2; Stage 3 (existence check + create-or-update) removes the
  dependency.
- Whether a withdrawn report retains `verdict = MALICIOUS` (Stage 2 delivery dependency, see
  "Stage 2 design").

## Test / verification

- `make lint-conventions`, `make test`.
- Table-driven tests per DEVGUIDE, testify require/assert, fake gRPC client (interface),
  real temp-DB KV. No live network.
