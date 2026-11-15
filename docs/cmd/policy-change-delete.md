# safedep policy change delete

Delete an open policy change. The policy does not change.

## Synopsis

```
safedep policy change delete CHANGE
```

## Examples

```bash
safedep policy change delete 19
```

```
✓ change 19 deleted
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The change was deleted. |
| non-zero | The request failed. |
