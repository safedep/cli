# safedep endpoint label set

Set `custom/` labels on endpoints. A policy override selects endpoints by their
labels. The label takes effect at the next policy fetch of each endpoint.

## Synopsis

```
safedep endpoint label set ENDPOINT... --label custom/KEY=VALUE...
```

## Flags

| Flag | Description |
|------|-------------|
| `--label <custom/key=value>` | A label. Repeatable, up to 32. |

An endpoint is a ULID, a unique ULID prefix, or a hostname that
`safedep endpoint list` cached. Give up to 100 endpoints. An admin writes only
`custom/` labels: the shared managed file sets `managed/` labels, and the tools
report `reported/` labels.

Until endpoint enrollment ships, an endpoint label is not an access boundary.
Select on `custom/` labels for a policy override that loosens the policy.

## Examples

```bash
safedep endpoint label set arjuns-mbp --label custom/team=release
```

```
✓ label set on 1 endpoint. It takes effect at the next policy fetch of each endpoint
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The labels were set. |
| non-zero | Validation, endpoint resolution or the request failed. |
