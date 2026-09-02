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

### GitHub references

A branch or tag of a GitHub repository is a mutable reference. The command
resolves it to the commit SHA it points to before it submits the scan, so the
scan stays bound to the code that was reviewed after the branch or tag moves.
This applies to every input form: a `pkg:github/owner/repo@ref` PURL, a GitHub
URL, and the explicit triple with a `github_actions` or `github_repository`
ecosystem. A version that is already a full commit SHA passes through without
a lookup. A GitHub URL without a `/tree/<ref>` suffix resolves to the head of
the default branch.

The resolved commit is the version of record: `get`, `list` and `show` apply
the same resolution, and the idempotency key uses the commit, so two runs
against the same ref dedupe only while the ref points at the same commit.

Resolution uses the GitHub API. Public repositories work without credentials
under the unauthenticated rate limit. Set `GITHUB_TOKEN` to raise the limit
and to reach private repositories. `GITHUB_BASE_URL` and `GITHUB_UPLOAD_URL`
point the lookup at a GitHub Enterprise server.

On a `MALWARE` verdict the full report is rendered inline (in `table` mode).
Any other verdict prints a short headline; fetch the full report with
`safedep package scan show`. In `plain` and `json` modes the output shape is
the same regardless of verdict.

## Arguments

| Argument | Description |
|----------|-------------|
| `<package-ref>` | A PURL (`pkg:npm/lodash@4.17.21`) or a GitHub repository URL. A GitHub branch or tag is resolved to its commit SHA. Alternative to the explicit `--ecosystem`/`--name`/`--version` flags. |

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

Scan a GitHub repository at a branch. The branch is pinned to its commit SHA:

```
safedep package scan run pkg:github/safedep/vet@main
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

A scan that failed because the target does not exist in the registry reports
`package not found in registry` with the target triple: check the package name
and version. Other failures surface the server-provided reason. A failed run
always exits through this error path, so it emits no JSON: scripts that need
the machine-readable `failure_code` alongside `failure_reason` should query
the scan with `safedep package scan get`, `list` or `show`, whose JSON output
carries both fields.
