# safedep project label set

Set `custom/` labels on projects. A policy override selects projects by their
labels. The label takes effect at the next check of each project.

## Synopsis

```
safedep project label set [PROJECT_NAME...] [--project-id ID]... --label custom/KEY=VALUE...
```

## Flags

| Flag | Description |
|------|-------------|
| `--label <custom/key=value>` | A label. Repeatable, up to 32. |
| `--project-id <id>` | A project ID instead of a name. Repeatable. |

Give up to 100 projects by exact name or ID. An admin writes only `custom/`
labels. The CLI rejects any other prefix before the request: the forge sync
writes `scm/` and `scm-property/` labels, and the server writes `safedep.io/`
labels. A value has at most 63 characters.

## Examples

```bash
safedep project label set acme/payments-api acme/ledger PAY/settlement --label custom/criticality=high
```

```
✓ label set on 3 projects. It takes effect at the next check of each project
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The labels were set. |
| non-zero | Validation, name resolution or the request failed. |
