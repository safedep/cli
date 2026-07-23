# safedep package scan show

## Synopsis

```
safedep package scan show <package-ref> [--save PATH]
safedep package scan show --scan-id ID [--save PATH]
```

## Description

`package scan show` renders the full analysis report of a completed package
scan: the verdict headline, the inference summary and details, file and project
evidence, and any warnings. Sections with no data are omitted, so reports for
non-library components (for example editor extensions) do not render empty
tables.

Address the scan either by package-ref (a PURL, a GitHub URL, or the
`--ecosystem`/`--name`/`--version` triple), in which case the newest scan for
that package is used, or directly by `--scan-id`.

The report is available only once the scan has completed. If the scan is still
in progress, the command reports the current status and exits non-zero.

## Arguments

| Argument | Description |
|----------|-------------|
| `<package-ref>` | A PURL or GitHub URL identifying the package whose newest scan report to show. Alternative to the explicit target flags or `--scan-id`. |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scan-id` | - | Address the scan directly by id, skipping target resolution. |
| `--ecosystem` | - | Package ecosystem. Use with `--name` and `--version`. |
| `--name` | - | Package name. |
| `--version` | - | Package version. |
| `--save` | - | Write the report JSON to this path. |

## Examples

Show the newest report for a package:

```
safedep package scan show pkg:pypi/requests@2.31.0
```

Show a report by scan id and save it:

```
safedep package scan show --scan-id scn_01JXY... --save report.json
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | A completed report was rendered. |
| non-zero | The scan is not yet completed, no scan exists for the target, or an RPC error. |
