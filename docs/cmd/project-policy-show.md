# safedep project policy show

Show the intended policy of one project: each field, its value, and its source.
The source is the SafeDep default policy, the policy base, or a policy override.
The output also has a table for the policy that each tool applied. In this
release no tool reports it for a project: the pull request check
(`pull-request-check`) resolves the policy at each check and sends no report, so
the table is empty.

## Synopsis

```
safedep project policy show [PROJECT_NAME | --project-id ID] [--field PATH]...
                            [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--project-id <id>` | The project ID instead of a name. |
| `--field <path>` | Show only this contract path, or the paths below it, as `scan_gate.fail_on` or `scan_gate`. Repeatable. |

## Examples

```bash
safedep project policy show acme/payments-api --field scan_gate.fail_on,package_admission.cooldown.min_age
```

```
Project         acme/payments-api
Policy          revision 47 · overrides payments (200)
Default policy  revision 1
Package rules   211 (184 trust, 27 block)

FIELD                               VALUE   SOURCE                     EXCEPTIONS
package_admission.cooldown.min_age  7d      override "payments" (200)  -
scan_gate.fail_on                   medium  override "payments" (200)  -

Applied policy, as each tool reported it
No tool or check reported the policy that it applies for this project yet.
```

## Output

`json` holds the subject, the revision, the matched overrides, the labels, the
rule counts, `fields` (path, value, source, exceptions) and `applied` (one entry
for each tool or check). `plain` writes one `path<TAB>value<TAB>source<TAB>exceptions`
line for each field and one `applied:<tool>` line for each report.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The policy was read. |
| non-zero | Validation, name resolution or the request failed. |
