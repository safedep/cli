# safedep endpoint activity list

## Synopsis

```
safedep endpoint activity list [flags]
```

## Description

Displays a unified feed of recent activity across endpoints reporting to SafeDep Cloud. The feed merges two event streams:

- **Guard events** - package install decisions made by the Package Management Guard (PMG), such as blocked or confirmed installs.
- **Inventory events** - new tool or configuration items detected by the inventory scanner.

By default the command shows guard events from the last 7 days (`--type guard --since 168h`) with the security-focused default action filter `blocked,cooldown-blocked`.

### Type semantics

| `--type` | Sources | Pagination |
|----------|---------|------------|
| `all`    | Guard + Inventory merged and sorted by descending timestamp | Approximate; `--page-token` is not threaded in merge mode |
| `guard`  | Guard events only | Exact; `--page-token` works |
| `inventory` | Inventory events only | Exact; `--page-token` works |

### Action vocabulary

Guard events carry one of the following actions:

| Value | Meaning |
|-------|---------|
| `blocked` | Install was denied by policy |
| `confirmed` | Install was allowed after user confirmation |
| `trusted` | Install was allowed because the package is trusted |
| `cooldown-blocked` | Install was denied during a cooldown window |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--type` | `guard` | Activity type: `all`, `guard`, or `inventory` |
| `--since` | `168h` | Trailing time window length, e.g. `24h`, `168h`, `30m` |
| `--endpoint` | _(none)_ | Filter by endpoint ULID or cached hostname; repeatable |
| `--action` | `blocked,cooldown-blocked` (when type includes guard) | Package action filter for guard events: `blocked`, `confirmed`, `trusted`, `cooldown-blocked`; repeatable |
| `--tool` | _(none)_ | Client-side filter by tool name (e.g. `claude-code`) |
| `--invocation` | _(none)_ | Scope results to a single invocation ID |
| `--limit` | `0` (server default) | Maximum number of rows to return |
| `--page-token` | _(none)_ | Continuation token from a prior response (single-source types only) |

## Output

Each row contains:

| Column | Description |
|--------|-------------|
| Time | Event timestamp (RFC3339) |
| Endpoint | Shortened endpoint ID |
| Type | `guard` or `inventory` |
| Tool | Reporting tool name |
| Summary | Human-readable event summary |

Supports `--output table` (default), `--output plain`, and `--output json`.

JSON output wraps rows under a `rows` key with an optional `next_page_token` field.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success (zero results is still success) |
| 1 | API or authentication error |

## Examples

Show recent guard activity from the last 7 days (default):

```
safedep endpoint activity list
```

Show all inventory detections from the last 7 days:

```
safedep endpoint activity list --type inventory --since 168h
```

Narrow to a single endpoint by hostname, confirmed installs only:

```
safedep endpoint activity list \
  --endpoint dev-laptop.example.com \
  --action confirmed
```

Page through guard events with an explicit limit:

```
safedep endpoint activity list --type guard --limit 50
safedep endpoint activity list --type guard --limit 50 --page-token <token>
```
