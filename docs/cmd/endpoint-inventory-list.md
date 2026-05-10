# safedep endpoint inventory list

## Synopsis

```
safedep endpoint inventory list [flags]
```

## Description

Displays the current inventory snapshot for one or more endpoints reporting to SafeDep Cloud. The snapshot is derived from raw inventory events by deduplicating on item identity and keeping the most recent event per identity within the requested time window.

This command is distinct from `endpoint activity list --type inventory`, which returns the raw event stream without deduplication. Use `endpoint inventory list` when you want a point-in-time picture of what tools, agents, extensions, and configuration files are present on an endpoint. Use `endpoint activity list --type inventory` when you need the full event history.

### Deduplication

Inventory events are deduplicated client-side by the `item_identity` field. When multiple events share the same identity the one with the latest timestamp is kept. Results are sorted alphabetically by name.

### Multi-endpoint warning

When `--endpoint` is not specified and the result page is full (i.e. the number of events returned equals `--limit`), the command prints a warning that the snapshot may be incomplete across the fleet. Use `--endpoint` to narrow scope or `--page-token` to continue paging.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--since` | `24h` | Trailing time window length, e.g. `24h`, `168h`, `30m` |
| `--endpoint` | _(none)_ | Filter by endpoint ULID or cached hostname; repeatable |
| `--kind` | _(none)_ | Filter by inventory kind (see Kind vocabulary below); repeatable |
| `--scope` | _(none)_ | Filter by scope: `system` or `project` |
| `--limit` | `0` (server default) | Maximum number of raw events to fetch before deduplication |
| `--page-token` | _(none)_ | Continuation token from a prior response |

### Kind vocabulary

| Flag value | Meaning |
|------------|---------|
| `mcp-server` | Model Context Protocol server |
| `coding-agent` | AI coding agent (e.g. Claude Code, Cursor) |
| `ai-extension` | AI-enabled editor extension |
| `cli-tool` | Command-line tool |
| `project-config` | Project-level configuration file |
| `browser-extension` | Browser extension |
| `ide-extension` | IDE extension (non-AI) |
| `agent-plugin` | Plugin installed into an AI agent |
| `agent-skill` | Skill registered with an AI agent |

## Output

Each row contains:

| Column | Description |
|--------|-------------|
| Endpoint | Shortened endpoint ID |
| Kind | Inventory item kind (kebab-case label) |
| Name | Display name of the inventory item |
| App | Application or package manager that owns the item |
| Scope | `system` or `project` |
| Last Seen | Timestamp of the most recent event for this identity |

Supports `--output table` (default), `--output plain`, and `--output json`.

JSON output wraps items under an `items` key with an optional `next_page_token` field. Each item carries `endpoint_id`, `kind`, `name`, `app`, `scope`, `config_path`, `metadata`, and `last_seen`.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success (zero results is still success) |
| 1 | API or authentication error |

## Examples

Show the current inventory for a single endpoint:

```
safedep endpoint inventory list --endpoint dev-laptop.example.com
```

Show all MCP servers and coding agents across all endpoints from the last 7 days:

```
safedep endpoint inventory list \
  --kind mcp-server \
  --kind coding-agent \
  --since 168h
```

Show only system-scoped items for a specific endpoint:

```
safedep endpoint inventory list \
  --endpoint dev-laptop.example.com \
  --scope system
```
