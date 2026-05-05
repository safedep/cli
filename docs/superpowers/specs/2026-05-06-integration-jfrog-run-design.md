# Design: `safedep integration jfrog run`

**Date:** 2026-05-06
**Status:** Approved
**Author:** Kunal Singh

---

## Overview

Add `safedep integration jfrog run --config <file>` — a long-running daemon that polls
SafeDep's malicious package API and pushes each verified malware finding to JFrog XRay
as a Custom Issue via the XRay REST API.

Phase 1 of the JFrog integration described in the SafeDep + JFrog Integration PRD. Runs
locally (on-prem or developer machine) with local config and credentials. No hosted
component, no credential management beyond what the CLI's existing auth system already
provides.

---

## Command

```
safedep integration jfrog run --config /path/to/config.yml
```

- Domain: `integration` (new top-level noun)
- Sub-noun: `jfrog`
- Verb: `run` (already in `AllowedVerbs`)
- `--config` is a required local flag on this command only

---

## Auth and dependency flow

Credentials are resolved entirely through the existing auth system. No new auth code.

```
RunE
  a.DataPlane()   →  *cloud.Client  (API key from --profile / keychain / env)
  a.DB()          →  *db.DB         (sqlite; first command to use this accessor)
  loadConfig()    →  Config

  newFeedService(
      malysisv1grpc.NewMalwareAnalysisServiceClient(client.Connection()),
      newJFrogPusher(cfg.JFrog),
      newCursorStore(db, a.Profile()),
  ).Run(cmd.Context())
```

`--profile` is a persistent root flag inherited by all subcommands. Running
`safedep --profile customer-a integration jfrog run --config ...` switches the API
key and cursor namespace without any extra wiring.

`RunE` does exactly three things: resolve deps, call `svc.Run`, return error. No
business logic in `RunE`.

---

## File layout

```
internal/cmd/integration/
  cmd.go                  # Register: adds `integration` parent to root
  jfrog/
    cmd.go                # Register: adds `jfrog` under integration
    run.go                # cobra command; RunE wiring only
    service.go            # FeedService: poll loop + fanout orchestration
    poller.go             # MaliciousPackagePoller: gRPC pagination + cursor save
    pusher.go             # JFrogPusher: HTTP POST to /xray/api/v1/events
    config.go             # YAML config schema + loader
    store.go              # sqlite cursor (integration_jfrog_cursor table)
    service_test.go       # FeedService unit tests with fakes
    run_test.go           # cobra convention tests
```

`internal/app/app.go` gains `App.DB()` — the first use of sqlite in the CLI. The
storage bootstrap (creating the db file, running migrations) lives in
`internal/storage/` as required by DEVGUIDE, introduced alongside this command.

`cmd/safedep/main.go` gets one new line: `integration.Register(root, a)`.

---

## Polling loop

1. Load `last_seen_at` from sqlite for the active profile (zero time on first run).
2. Call `MalwareAnalysisService.ListPackageAnalysisRecords` with:
   - `start_from = last_seen_at`
   - `filter.only_malware = true`
   - `filter.only_verified = true`
3. Page through all results via `next_page_token` until the token is empty.
4. For each `AnalysisRecord`, call `JFrogPusher.Push`.
5. After each page, update the cursor to the `created_at` of the latest record in that
   page. Saving per-page (not per-batch) means a crash replays at most one page, and
   the JFrog push is idempotent by deterministic issue `id`, so re-delivery is safe.
6. Sleep `source.poll_interval`, repeat.

The loop runs until the context is cancelled (SIGINT / SIGTERM).

---

## sqlite cursor

Table created on first run:

```sql
CREATE TABLE IF NOT EXISTS integration_jfrog_cursor (
    profile      TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    PRIMARY KEY (profile)
);
```

`last_seen_at` is stored as RFC3339 UTC. Keyed by profile name so multiple profiles
(tenants) can run independently on the same machine.

---

## JFrog XRay pusher

**Endpoint:** `POST https://<jfrog.url>/xray/api/v1/events`

**Headers:**
```
Authorization: Bearer <access_token>
Content-Type: application/json
```

**Payload mapping** from `AnalysisRecord`:

| JFrog field | Source | Notes |
|---|---|---|
| `id` | `"SAFEDEP-MAL-" + truncate(package.name, 20)` | Max 32 chars total; must not start with "Xray" |
| `type` | `"Security"` | Fixed |
| `provider` | `"SafeDep"` | Must not be "JFrog" |
| `package_type` | `ecosystemToJFrog(package.ecosystem)` | See mapping below |
| `severity` | `"Critical"` | Fixed for verified malware |
| `issue_kind` | `1` | 1 = malicious package (sets `malicious_package: "True"` in XRay) |
| `summary` | `"MALICIOUS PACKAGE: <name> contains malicious code"` | |
| `description` | `"<name> <version> identified as malicious by SafeDep."` | |
| `components[0].id` | `package.name` | Name only — NOT URI format (`npm://name:ver`) |
| `components[0].vulnerable_versions` | `["[" + version + "]"]` | Bracket notation required — without brackets XRay silently drops the record |
| `sources[0].source_id` | `"safedep-threat-intel"` | Fixed |
| `properties` | `{}` | Empty for now |

**Ecosystem mapping** (SafeDep → JFrog `package_type`):

| SafeDep | JFrog |
|---|---|
| `npm` | `npm` |
| `pypi` | `pypi` |
| `maven` | `maven` |
| `go` | `go` |
| `nuget` | `nuget` |
| `rubygems` | `gem` |
| (default) | `generic` |

Non-2xx responses are logged via `dry/log` and the record is skipped (best-effort
delivery). The cursor still advances after each page. A package that consistently fails
to push will not block new packages from being delivered.

---

## Config schema

```yaml
source:
  poll_interval: 60s       # sleep between poll cycles; default 60s if omitted

jfrog:
  url: https://company.jfrog.io   # base URL; no trailing slash
  access_token: TOKEN             # Bearer token; env SAFEDEP_JFROG_ACCESS_TOKEN overrides
```

`access_token` resolution order: `SAFEDEP_JFROG_ACCESS_TOKEN` env var, then config
file value. This keeps credentials out of config files checked into source control.

`--config` is required. Startup fails fast with a clear error if the file is missing
or malformed, or if `jfrog.url` is empty.

---

## Observability

The command logs to stderr via `dry/tui` (Info / Warning / Error). Structured JSON
logging is available when `SAFEDEP_OUTPUT=json` or the TTY is detected as an agent
environment. Each poll cycle logs: records fetched, records pushed, cursor value.

`/health` and `/metrics` HTTP endpoints are out of scope for Phase 1. They are
noted in the PRD for Phase 2 (hosted deployment).

---

## Testing

- `service_test.go`: table-driven tests for `FeedService` using hand-rolled fakes for
  `MalwareAnalysisServiceClient` and `JFrogPusher`. Tests cover: empty response, single
  page, multi-page pagination, cursor advance, push error (continue), context cancel.
- `run_test.go`: cobra convention tests (verb in allowlist, Short/Long non-empty).
- No real network or DB in unit tests. Integration tests (real gRPC + real JFrog) are
  out of scope for Phase 1.

---

## Out of scope (Phase 1)

- S2 stream-based consumption (polling only)
- Slack or generic webhook sinks
- `/health` and `/metrics` HTTP endpoints
- Hosted/cloud deployment mode
- Per-tenant configuration of which packages to filter
- Notifications when a malicious package is blocked
