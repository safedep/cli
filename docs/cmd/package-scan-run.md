# safedep package scan run

## Synopsis

```
safedep package scan run <package-ref> [--ecosystem ECO --name NAME --version VER] [--wait] [--timeout DUR] [--rescan] [--save PATH]
```

## Description

`package scan run` submits a package version to SafeDep Cloud for on-demand
malware scanning. Scanning is asynchronous: the command submits the scan and,
by default, waits for it to reach a terminal state before rendering the
verdict.

The package is identified by an ecosystem, name and version. Provide it either
as a positional reference (a PURL such as `pkg:npm/lodash@4.17.21`, or a GitHub
repository URL) or as the explicit `--ecosystem`/`--name`/`--version` triple.
The explicit triple is the canonical form and works for every ecosystem; PURL
is a convenience shortcut for the ecosystems it covers. The ecosystem is
required: SafeDep Cloud uses it to select the scan workflow.

By default the command derives a deterministic idempotency key from the target,
so repeat runs of the same package version reuse the existing scan rather than
creating duplicates. Pass `--rescan` to force a fresh scan.

On a `MALWARE` verdict the full report is rendered inline (in `table` mode).
Any other verdict prints a short headline; fetch the full report with
`safedep package scan show`. In `plain` and `json` modes the output shape is
the same regardless of verdict.

## Arguments

| Argument | Description |
|----------|-------------|
| `<package-ref>` | A PURL (`pkg:npm/lodash@4.17.21`) or a GitHub repository URL. Alternative to the explicit `--ecosystem`/`--name`/`--version` flags. |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--ecosystem` | - | Package ecosystem (npm, pypi, vscode, openvsx, ...). Use with `--name` and `--version`. |
| `--name` | - | Package name. |
| `--version` | - | Package version. |
| `--wait` | `true` | Wait for the scan to reach a terminal state. Use `--wait=false` to submit and return immediately. |
| `--timeout` | `5m` | Maximum time to wait for a verdict. |
| `--rescan` | `false` | Force a fresh scan instead of reusing an existing one. |
| `--save` | - | Write the completed report JSON to this path. Requires waiting (incompatible with `--wait=false`). |

## Examples

Scan an npm package by PURL and wait for the verdict:

```
safedep package scan run pkg:npm/lodash@4.17.21
```

Scan a VS Code extension using the explicit triple:

```
safedep package scan run --ecosystem vscode --name publisher.extension --version 1.2.3
```

Submit without waiting (for scripts and agents):

```
safedep package scan run pkg:pypi/requests@2.31.0 --wait=false
```

Force a fresh scan and save the report:

```
safedep package scan run pkg:npm/left-pad@1.3.0 --rescan --save report.json
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The scan was submitted (with `--wait=false`) or reached a terminal state. |
| non-zero | An RPC error, a `FAILED` scan, or a `--timeout` expiry. The verdict does not affect the exit code. |
