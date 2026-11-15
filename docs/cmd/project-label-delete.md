# safedep project label delete

Delete `custom/` labels from projects. The change takes effect at the next check
of each project.

## Synopsis

```
safedep project label delete [PROJECT_NAME...] [--project-id ID]... --key custom/KEY...
```

## Flags

| Flag | Description |
|------|-------------|
| `--key <custom/key>` | A label key. Repeatable, up to 32. |
| `--project-id <id>` | A project ID instead of a name. Repeatable. |

## Examples

```bash
safedep project label delete acme/ledger --key custom/criticality
```

```
✓ label deleted from 1 project. It takes effect at the next check of each project
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The labels were deleted. |
| non-zero | Validation, name resolution or the request failed. |
