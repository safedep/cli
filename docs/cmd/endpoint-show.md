# safedep endpoint show

## Synopsis

```
safedep endpoint show <endpoint> [--since DURATION]
```

## Description

`endpoint show` fetches a detailed view of a single endpoint registered with SafeDep Cloud. It composes three RPCs: `GetEndpoint` (identity, last sync, per-tool event volumes, last invocation context), `ListEndpointPackageGuardEvents` (up to five recent blocked package installs in the requested window), and `ListEndpointInventoryEvents` (distinct inventory item count over the past 24 hours).

If the guard-events or inventory-events RPCs fail, the command logs a warning and continues, returning the core endpoint data. Exit code is non-zero only when the primary `GetEndpoint` RPC fails.

The `<endpoint>` argument is resolved as follows: if it is a ULID it is used directly; otherwise the local directory cache (populated by `safedep endpoint list`) is searched for a matching hostname or identifier. The cache is refreshed with the returned data on each successful call.

## Arguments

| Argument | Description |
|----------|-------------|
| `<endpoint>` | ULID of the endpoint, or a hostname/identifier previously cached by `endpoint list`. |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--since DURATION` | `168h` | Trailing window length used for per-tool event volumes and recent block lookup. Accepts Go duration strings, e.g. `24h`, `168h`, `720h`. |

## Examples

Show an endpoint by its ULID:

```
safedep endpoint show 01KR0EKN6PMW0ZRFRN992H1PKX
```

Show an endpoint by hostname after populating the cache with `endpoint list`:

```
safedep endpoint list
safedep endpoint show my-laptop
```

Show with a narrower event window:

```
safedep endpoint show my-laptop --since 24h
```

## Output formats

| Format | Description |
|--------|-------------|
| table (default) | Card-style layout with sections: identity fields, per-tool event volumes (if any), last invocation context (if present), recent blocked installs (if any). |
| plain (`--output plain`) | Key/value lines suitable for scripting: `id`, `hostname`, `os`, `last_sync`, `inventory_count`, zero or more `tool` lines, zero or more `block` lines. Tab-separated. |
| json (`--output json`) | Full structure: `endpoint` object, `recent_blocks` array, `inventory_count` integer. |

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success. |
| 1 | Endpoint not found, resolution failed, or primary RPC error. |
