# safedep endpoint label delete

Delete `custom/` labels from endpoints. The change takes effect at the next
policy fetch of each endpoint.

## Synopsis

```
safedep endpoint label delete ENDPOINT... --key custom/KEY...
```

## Flags

| Flag | Description |
|------|-------------|
| `--key <custom/key>` | A label key. Repeatable, up to 32. |

## Examples

```bash
safedep endpoint label delete arjuns-mbp --key custom/team
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The labels were deleted. |
| non-zero | Validation, endpoint resolution or the request failed. |
