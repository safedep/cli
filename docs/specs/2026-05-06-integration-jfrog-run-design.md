# Design: `safedep integration jfrog run`

**Date:** 2026-05-06
**Status:** Approved (POC)
**Author:** Kunal Singh

---

## Overview

Add `safedep integration jfrog run --config <file>` — a long-running daemon that polls
SafeDep's malicious package API and pushes each verified malware finding to JFrog XRay
as a Custom Issue via the XRay REST API.

POC scope: poll, push, persist cursor. No tests, no health endpoints, no metrics.

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
  loadConfig()    →  Config

  newFeedService(
      malysisv1grpc.NewMalwareAnalysisServiceClient(client.Connection()),
      newJFrogPusher(cfg.JFrog),
      newCursorStore(cfg.Source.CursorFile),
  ).Run(cmd.Context())
```

`--profile` is inherited from the root command. `RunE` does exactly three things:
resolve deps, call `svc.Run`, return error.

---

## File layout

```
internal/cmd/integration/
  cmd.go        # Register: adds `integration` parent to root
  jfrog/
    cmd.go      # Register: adds `jfrog` under integration
    run.go      # cobra command; RunE wiring only
    service.go  # FeedService: poll loop + push orchestration
    poller.go   # MaliciousPackagePoller: gRPC pagination + cursor save
    pusher.go   # JFrogPusher: HTTP POST to /xray/api/v1/events
    config.go   # YAML config schema + loader
    store.go    # file-based cursor (JSON file)
```

`cmd/safedep/main.go` gets one new line: `integration.Register(root, a)`.

---

## Polling loop

1. Load `last_seen_at` from cursor file (zero time if file absent — polls from beginning).
2. Call `MalwareAnalysisService.ListPackageAnalysisRecords` with:
   - `start_from = last_seen_at`
   - `filter.only_malware = true`
   - `filter.only_verified = true`
3. Page through all results via `next_page_token` until token is empty.
4. For each `AnalysisRecord`, call `JFrogPusher.Push`.
5. After each page, write the `created_at` of the latest record to the cursor file.
6. Sleep `source.poll_interval`, repeat.

Loop runs until context is cancelled (SIGINT / SIGTERM).

---

## File-based cursor

JSON file at `source.cursor_file` (default `~/.safedep/integration-jfrog-cursor.json`):

```json
{"last_seen_at": "2026-05-06T10:00:00Z"}
```

Written atomically (temp file + rename) after each page.

---

## JFrog XRay pusher

**Endpoint:** `POST https://<jfrog.url>/xray/api/v1/events`

**Headers:**
```
Authorization: Bearer <access_token>
Content-Type: application/json
```

**Payload mapping** from `AnalysisRecord`:

| JFrog field | Value | Notes |
|---|---|---|
| `id` | `"SAFEDEP-MAL-" + truncate(package.name, 20)` | Max 32 chars; must not start with "Xray" |
| `type` | `"Security"` | Fixed |
| `provider` | `"SafeDep"` | Must not be "JFrog" |
| `package_type` | `ecosystemToJFrog(package.ecosystem)` | See mapping below |
| `severity` | `"Critical"` | Fixed for verified malware |
| `issue_kind` | `1` | Sets `malicious_package: "True"` in XRay |
| `summary` | `"MALICIOUS PACKAGE: <name> contains malicious code"` | |
| `description` | `"<name> <version> identified as malicious by SafeDep."` | |
| `components[0].id` | `package.name` | Name only — NOT URI format |
| `components[0].vulnerable_versions` | `["[<version>]"]` | Bracket notation required — without it XRay silently drops the record |
| `sources[0].source_id` | `"safedep-threat-intel"` | Fixed |
| `properties` | `{}` | |

**Ecosystem mapping** (SafeDep → JFrog `package_type`):

`npm→npm`, `pypi→pypi`, `maven→maven`, `go→go`, `nuget→nuget`, `rubygems→gem`, default→`generic`

Non-2xx responses are logged via `dry/log` and the record is skipped. The cursor still
advances after the page.

---

## Config schema

```yaml
source:
  poll_interval: 60s                                        # default 60s if omitted
  cursor_file: ~/.safedep/integration-jfrog-cursor.json    # default if omitted

jfrog:
  url: https://company.jfrog.io   # base URL; no trailing slash
  access_token: TOKEN             # env SAFEDEP_JFROG_ACCESS_TOKEN overrides
```

`--config` is required. Startup fails fast if the file is missing, malformed, or
`jfrog.url` is empty.

---

## Out of scope (POC)

- Tests
- `/health` and `/metrics` endpoints
- S2 stream-based consumption
- Slack or generic webhook sinks
- Hosted/cloud deployment
