# safedep package scan get

## Synopsis

```
safedep package scan get <package-ref>
safedep package scan get --scan-id ID
```

## Description

`package scan get` returns the status and verdict of a package scan without
the full report. It is the cheap path for polling and scripting.

Address the scan either by package-ref (a PURL, a GitHub URL, or the
`--ecosystem`/`--name`/`--version` triple), in which case the newest scan for
that package is returned, or directly by `--scan-id`. The `--scan-id` path is
intended for agents that just submitted a scan and hold its id.

## Arguments

| Argument | Description |
|----------|-------------|
| `<package-ref>` | A PURL or GitHub URL identifying the package whose newest scan to fetch. Alternative to the explicit target flags or `--scan-id`. |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scan-id` | - | Address the scan directly by id, skipping target resolution. |
| `--ecosystem` | - | Package ecosystem. Use with `--name` and `--version`. |
| `--name` | - | Package name. |
| `--version` | - | Package version. |

## Examples

Check the newest scan for a package:

```
safedep package scan get pkg:npm/lodash@4.17.21
```

Check a scan by id:

```
safedep package scan get --scan-id scn_01JXY...
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The scan was found and its status rendered. |
| non-zero | No scan exists for the target, or an RPC error occurred. |
