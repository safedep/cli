# safedep integration crowdstrike run

Long-running daemon that streams endpoint package-guard **block events** from
SafeDep and logs them. Two signals are covered, treated as one event stream:

- **Malicious package blocks** (a package identified as malware and blocked).
- **Dependency cooldown blocks** (a package blocked for being published too
  recently).

Events are pulled incrementally by cursor, so each run resumes where it stopped.
This is Stage 1: the events are logged. A later stage will push them to
CrowdStrike SIEM.

## Synopsis

```
safedep integration crowdstrike run
```

## Quick start

```bash
# 1. Authenticate with SafeDep (once)
safedep auth login

# 2. Stream and log new block events
safedep integration crowdstrike run
```

## Flags

| Flag | Required | Default | Description |
|---|---|---|---|
| `--poll-interval` | no | `5m` | Sleep duration between sync cycles (`30s`, `5m`, `1h`). |
| `--backfill` | no | `0` | First-run window used to seed the cursor. `0` starts fresh from now. |
| `--profile` | no | `"default"` | SafeDep credential profile (inherited from root). |

`--backfill` takes a Go duration, so use hours for multi-day windows (e.g.
`--backfill 168h` for 7 days). It only affects the **first** run on a fresh
install: with no stored cursor, the command requests events after
`now - backfill`. Once a cursor exists, `--backfill` is ignored and the command
resumes from the last processed event. At startup the command logs which mode it
uses: resuming from the saved cursor, or starting fresh (with the backfill
window when set).

## Output and logs

The command keeps output and logs separate.

- **Output** is the result: one block event.
- **Logs** are operational: sync cycle, startup mode, and errors.

`-o json` (or `--output json`) is a request for machine output. In this mode the
command prints only the event records, as JSONL on stdout, one object per line.
It suppresses every log. Without `-o json`, the command prints results and logs
to stderr for people.

```bash
safedep integration crowdstrike run -o json
```

```json
{"event":"endpoint_package_blocked","action":"blocked","event_id":"evt-1","timestamp":"2026-09-15T10:00:00Z","endpoint_id":"ep-1","endpoint_name":"web-01","tool_name":"pmg","package":"left-pad","ecosystem":"npm","version":"1.0.0","is_malware":true,"is_verified":true,"analysis_id":"an-1"}
{"event":"endpoint_package_blocked","action":"cooldown_blocked","event_id":"evt-2","timestamp":"2026-09-15T10:01:00Z","endpoint_name":"web-01","tool_name":"pmg","package":"shiny-pkg","ecosystem":"npm","version":"0.0.1","cooldown":{"cooldown_days":7,"days_since_publish":4,"days_remaining":3}}
```

Every record is `event: endpoint_package_blocked`, distinguished by `action`
(`blocked` or `cooldown_blocked`). The `cooldown` object is present only on
cooldown events.

## Behaviour

- **First run.** With no stored cursor the command starts fresh from now
  (`--backfill 0`). It does not pull historical events unless you set
  `--backfill`.
- **Both actions.** Malicious blocks and cooldown blocks are pulled together and
  logged the same way, tagged by `action`.
- **Resume.** The cursor is stored per SafeDep profile. Restarting resumes from
  the last processed event. Switching `--profile` switches the cursor.
  Re-process from a chosen point with
  [`cursor set`](./integration-crowdstrike-cursor-set.md), or from scratch with
  [`cursor remove`](./integration-crowdstrike-cursor-remove.md).

## SafeDep authentication

The command reads the SafeDep control plane (`cloud.safedep.io`), the same
service as the `safedep endpoint` commands. It authenticates with an **OAuth
session**, so log in with the device flow:

```bash
safedep auth login
```

Confirm the code in your browser to complete login. The session is stored in the
keychain per profile and refreshed automatically. An API-key-only login does not
work for this control-plane service, so use `safedep auth login` (OAuth).

## Subscription

This integration requires the **endpoint management add-on**. If the tenant does
not have it, the command stops with a message pointing to the
[pricing page](https://safedep.io/pricing).
