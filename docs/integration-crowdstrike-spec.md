# Spec: `integration crowdstrike run` (PMG endpoint block events poller)

Status: Stage 1 implemented. This is the design record; Stage 2 (CrowdStrike SIEM
push) and Stage 3 remain future work.

## Goal

A cursor-based poller, modeled on `integration jfrog`, that incrementally pulls
new **PMG endpoint package-block events** from SafeDep and, for now, **logs**
them. The two signals we care about, treated uniformly (they are just events):

- **Malicious package blocks** — `PmgPackageAction.BLOCKED (1)`.
- **Cooldown blocks** — `PmgPackageAction.COOLDOWN_BLOCKED (4)`.

## Staged delivery

| Stage | Scope | State |
|---|---|---|
| **Stage 1** | Poll `ListEndpointPackageGuardEvents` by cursor, filter to the two block actions, send each new event to the **`printClient`** sink adapter (logs it: JSONL on stdout under `-o json`, human line on stderr). | **this spec** |
| **Stage 2** | Implement a **`crowdstrikeClient`** sink adapter (POST to the CrowdStrike SIEM ingest API) and swap it in behind the same port. `--dry-run` selects `printClient` for a log-only preview. | future |

The command is named `crowdstrike` now (not `endpoint`/`pmg`) so Stage 2 lands
under the same noun without a rename. The sink is a **port** from day one
(exactly like jfrog's `xrayClient`), so Stage 2 only adds one adapter and a
wiring line, changing nothing in the source, service, or cursor.

## Source (settled)

- **RPC:** `EndpointManagementService.ListEndpointPackageGuardEvents`
  (`controltowerv1grpc.NewEndpointManagementServiceClient`).
- **Transport / auth:** `a.ControlPlane().Connection()` with an **OAuth session**
  (`safedep auth login` device flow, token refresh handled by the app). This is a
  control-plane service (`cloud.safedep.io`), the same one the `safedep endpoint`
  commands use. Verified: the data plane (api.safedep.io, API key) returns
  `Unimplemented / 404` for it, so it is not plane-interchangeable.
- **Request per page:**
  - `Filter.Pmg.EventTypes = [PACKAGE_DECISION]`,
    `Filter.Pmg.PackageActions = [BLOCKED, COOLDOWN_BLOCKED]`.
    `EndpointIds` left empty (all endpoints), `InvocationId` unset.
  - `TimeRange.Start = cursor watermark` (see Cursor). `End = now`, captured once
    per drain. Both are required and the API rejects `start >= end`, so a fresh
    `start == now` is nudged below `end`.
  - `Pagination.PageSize = 100` (service-capped), `SortOrder = ASCENDING`,
    `PageToken` advances within a drain.
- **Response:** `Events []PackageGuardEvent`, `Pagination.NextPageToken`.
- **Event fields used:** `EventId`, `Timestamp`, `EndpointId`, `EndpointName`,
  `ToolName`, and `PmgEvent.PackageDecision`:
  `PackageVersion` (`Package.Name`, `Package.Ecosystem`, version), `Action`,
  `IsMalware`, `IsVerified`, `AnalysisId`, `Cooldown` (`PublishDate`,
  `CooldownDays`, `DaysSincePublish`, `DaysRemaining`).

## Cursor semantics

Profile-scoped KV, namespace `integration-crowdstrike`, key `cursor`. Unlike the
jfrog feed (mutable reports keyed on `updated_at`), these events are **immutable
and append-only**, so a `Timestamp` watermark is correct and there is no
re-delivery-on-change concern.

```go
type cursorState struct {
    LastSeenAt       time.Time `json:"last_seen_at"`
    LastSeenEventIDs []string  `json:"last_seen_event_ids"` // event ids at exactly LastSeenAt
}
```

Rules:

- `TimeRange.Start = LastSeenAt`. First run: `now - backfill` (default backfill
  `0` => `now`, fresh). Never omitted.
- **Boundary dedup:** the API does not document whether `Start` is inclusive.
  We skip any event whose `EventId` is in `LastSeenEventIDs`, so an inclusive
  `Start` (or two events sharing a timestamp across a page boundary) never
  re-logs. This matters for the future SIEM ingest where duplicates are costly;
  the cost now is one `[]string` field, normally tiny.
  `ponytail:` if the API guarantees exclusive `Start`, drop `LastSeenEventIDs`.
- Ascending order + fixed `Start` for the whole drain (only `PageToken` moves),
  so an interrupted drain resumes gap-free.
- Save once, after the full drain: `LastSeenAt = max event Timestamp`,
  `LastSeenEventIDs = ids at that max`. Forward-only.
- First run that saw nothing: anchor `LastSeenAt = Start` so the window does not
  slide forward by `poll-interval` each cycle (same fix as jfrog).
- Decode failure => reset + warn; DB error => propagate + retry next cycle.

## Command surface

```
safedep integration crowdstrike run
  --poll-interval  duration  sleep between drains (default 5m)
  --backfill       duration  first-run window; 0 = fresh from now (default 0)
safedep integration crowdstrike cursor set <rfc3339>
safedep integration crowdstrike cursor remove
```

No JFrog-style URL/token flags (no external sink in Stage 1). SafeDep auth is the
active profile / `SAFEDEP_API_KEY` + `SAFEDEP_TENANT_ID`, same as every
data-plane command. No `--dry-run` in Stage 1 (logging is the behavior; dry-run
arrives with the Stage 2 push). `cursor set/remove` mirror jfrog for re-processing
a window.

## Sink port and adapters (mirrors jfrog `xrayClient`)

The service never knows where events go. It pushes each event through a port:

```go
type eventSink interface {
    // validate proves the sink is reachable/authorized, once at startup.
    validate(ctx context.Context) error
    // send delivers one event. The adapter owns mapping + emitting its own
    // result line. Errors are best-effort (logged, non-fatal) in the service.
    send(ctx context.Context, event *controltowerv1.ListEndpointPackageGuardEventsResponse_PackageGuardEvent) error
}
```

Adapters (same shape as jfrog's `jfrogClient` + `printClient`):

| Adapter | Stage | `validate` | `send` |
|---|---|---|---|
| `printClient` | **1 (now)** | logs "logging mode, nothing is sent" | maps the event and emits it via the reporter (see Output) |
| `crowdstrikeClient` | 2 (future) | checks CrowdStrike ingest connectivity/creds | POSTs the event to the CrowdStrike SIEM ingest API, emits a result on success |

`run.go` wires `printClient` now; Stage 2 swaps `crowdstrikeClient` (e.g. when
CrowdStrike creds are configured, `--dry-run` forces `printClient`), exactly like
jfrog's `buildSourceAndClient`. Following jfrog, **no neutral DTO**: the port
takes the raw `*PackageGuardEvent` and each adapter maps it (a translation layer
for one wire type is dead weight). Ecosystem mapping and event->fields helpers
live beside the port in `client.go`.

## Output (logging via `printClient`)

`printClient.send` reuses the reporter output/log split. Each new block event is
a **result**:

- Human (default): one line on stderr, e.g.
  `Blocked: left-pad@1.0.0 (npm) malware on host web-01 [evt_...]`
  `Cooldown: left-pad@1.0.0 (npm) 3d remaining on host web-01 [evt_...]`
- `-o json`: one JSONL record on stdout:

```json
{"event":"endpoint_package_blocked","action":"blocked","event_id":"...",
 "timestamp":"...","endpoint_id":"...","endpoint_name":"web-01",
 "tool_name":"pmg","package":"left-pad","ecosystem":"npm","version":"1.0.0",
 "is_malware":true,"is_verified":true,"analysis_id":"...",
 "cooldown":{"publish_date":"...","cooldown_days":7,"days_since_publish":4,"days_remaining":3}}
```

One `event` name (`endpoint_package_blocked`) with an `action` field
(`blocked` / `cooldown_blocked`); `cooldown` object present only for cooldown
events. Poll-cycle lines, "starting", resume/backfill notices are logs (stderr in
human mode, suppressed under `-o json`), exactly as jfrog.

## Error handling

Mirror jfrog's transient-vs-fatal split in the poll loop:

- `PermissionDenied` => fatal, friendly "endpoint management / PMG not enabled for
  this tenant" message; stop, don't loop.
- Unauthenticated / API-key errors => fatal auth message (how to log in / set env).
- Any other error => log + retry next cycle.
- `ctx.Done()` => clean stop.

## Architecture and reuse

**Chosen: Option B** — copy the ~small generic pieces into the new `crowdstrike`
package, leave the shipped `jfrog` untouched (zero risk to a live feature). The
duplicated pieces are small and stable: the reporter (output/log split), the
cursor store over `*storage.KV`, and the `callbackError`/source seam. A shared
kit (Option A: extract to `internal/.../streamkit` and refactor jfrog onto it)
was considered and declined for now to avoid churning a shipped integration; it
stays available as a later cleanup if a third consumer appears.

Files, new package `internal/cmd/integration/crowdstrike/`:

| File | Responsibility |
|---|---|
| `cmd.go` | Register `crowdstrike` under `integration`; `run` + `cursor` verbs. |
| `run.go` | Flag/env resolve; build source + sink; hand to service. |
| `source.go` | `ListEndpointPackageGuardEvents` paging + cursor; source seam (copied). |
| `service.go` | Route each delivered event to the sink (best-effort). |
| `client.go` | `eventSink` port + mapping helpers (ecosystem, event->fields). |
| `printclient.go` | `printClient` sink adapter: maps + emits via the reporter. |
| `reporter.go` | Output/log split (copied from jfrog, own `jsonEvent`). |
| `cursor.go` | Cursor store over `*storage.KV[cursorState]` (copied). |
| `types.go` | Config DTO. |

`crowdstrikeClient` (the real sink adapter) is a Stage 2 file, not built now.

`integration/cmd.go` gains `crowdstrike.Register(cmd, a)` next to `jfrog.Register`.

## Out of scope (Stage 1)

- Any push to CrowdStrike (Stage 2).
- `--dry-run` (meaningless with no external sink).
- Session-summary / other PMG event types; only `PACKAGE_DECISION` with the two
  block actions.

## Open items / assumptions to verify at build

1. ~~Data-plane serves the service~~ RESOLVED: it does not. This is a
   control-plane service; use `a.ControlPlane()` (OAuth), matching `safedep endpoint`.
2. `TimeRange.Start` inclusivity — the `EventId` dedup makes us correct either
   way; confirm to decide whether `LastSeenEventIDs` can be dropped.
3. Service page-size cap (assume 100).
4. Shape/stability of `EventId` (used as the dedup key and in output).

## Tests

Mirror jfrog: table-driven, fake grpc `EndpointManagementServiceClient`, real
temp-DB `*storage.KV` for the cursor. Cover: filter/request construction,
ascending paging across a page boundary, cursor advance + boundary `EventId`
dedup, backfill first-run, transient-retry vs fatal (PermissionDenied /
Unauthenticated), and JSONL vs human output.
