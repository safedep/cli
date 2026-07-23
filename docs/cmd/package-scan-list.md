# safedep package scan list

## Synopsis

```
safedep package scan list [--ecosystem ECO --name NAME --version VER] [--limit N] [--page-token TOKEN]
```

## Description

`package scan list` lists package scans for the active tenant, newest first.

Optionally filter to one package version with the
`--ecosystem`/`--name`/`--version` triple. The server filter matches an exact
package version, so all three must be provided together when filtering.

Results are paginated. When more results are available, the table footer
prints the `--page-token` value to pass to the next call.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--ecosystem` | - | Filter: package ecosystem. Requires `--name` and `--version`. |
| `--name` | - | Filter: package name. |
| `--version` | - | Filter: package version. |
| `--limit` | server default | Page size. |
| `--page-token` | - | Continuation token from a prior response. |

## Examples

List recent scans:

```
safedep package scan list
```

List scans for a specific package version:

```
safedep package scan list --ecosystem npm --name lodash --version 4.17.21
```

Continue to the next page:

```
safedep package scan list --page-token eyJ...
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The listing was rendered (including an empty listing). |
| non-zero | An invalid filter (partial target) or an RPC error. |
