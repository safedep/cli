# safedep endpoint list

List endpoints reporting to SafeDep Cloud, with optional filters for capability, blocked installs, silent duration, and identity search.

## Synopsis

```
safedep endpoint list [flags] [--output table|plain|json] [--profile <name>]
```

## Flags

| Flag | Description |
|---|---|
| `--since <duration>` | Trailing window length for event counts (default `24h`). Accepts Go duration strings: `24h`, `168h`, `30m`. |
| `--capability <name>` | Filter to endpoints that advertise this capability. Repeatable. Accepted values: `guard`, `tracer`, `advisor`, `inventory`. |
| `--blocked` | Only endpoints with at least one blocked package-guard install in the window. |
| `--silent-for <duration>` | Only endpoints whose last sync is older than this duration (client-side filter; best-effort within `--limit`). |
| `--search <substring>` | Case-insensitive substring match on hostname or name (client-side filter; best-effort within `--limit`). |
| `--limit <n>` | Page size. Server default applies when `0` (the default). |
| `--page-token <token>` | Continuation token from a prior response, to fetch the following page. |

Inherits root flags `--output` and `--profile`.

## Examples

List all endpoints active in the last 24 hours (default window):

```bash
safedep endpoint list
```

List endpoints running the package-guard capability with at least one blocked install this week:

```bash
safedep endpoint list --since 168h --capability guard --blocked
```

Find endpoints that have not checked in for more than 7 days:

```bash
safedep endpoint list --silent-for 168h
```

Search by hostname substring and output JSON:

```bash
safedep endpoint list --search laptop --output json
```

Paginate through a large fleet:

```bash
TOK=$(safedep endpoint list --limit 50 --output json | jq -r .next_page_token)
safedep endpoint list --limit 50 --page-token "$TOK"
```

## Output

The table and plain modes show one row per endpoint. The JSON mode includes the full list and a `next_page_token` when more pages are available.

| Mode | Columns |
|---|---|
| `table` | ID (first 8 chars), Hostname, OS/Arch, Capabilities, Last Sync, Blocked, Inventory |
| `plain` | Tab-separated: ID, Hostname, OS/Arch, Capabilities, Last Sync, Blocked, Inventory |
| `json` | `{ "endpoints": [...], "next_page_token": "..." }` |

When no endpoints match, `table` and `plain` print `no endpoints`. JSON returns an empty `endpoints` array.

As a side effect, `endpoint list` populates the local endpoint directory used by `endpoint show` for hostname-to-ID resolution.

## Client-side filters

`--silent-for` and `--search` are applied after the server response, within the fetched page. Combine with `--limit` to control how many endpoints the server returns before these filters narrow the result. For fleet-wide silent detection, omit `--limit` and let the server stream all results.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

- `0` on success.
- `1` on any failure: missing credentials, invalid capability name, server error.
