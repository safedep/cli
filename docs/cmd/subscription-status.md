# safedep subscription status

## Synopsis

```
safedep subscription status
```

## Description

`subscription status` shows the tenant account's plan state: subscription status
(`FREE`, `ACTIVE_TRIAL`, `ACTIVE`, `PAST_DUE`, `ACTIVE_PENDING_CANCELLATION`), tier,
trial days remaining when in a trial, and on-demand billing state. It is the hub
command; other subscription commands point back to it.

Entitlements are not shown by default; pass `--entitlements` to include them (in all
output modes).

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--entitlements` | `false` | Also list the account's entitlements. |

## Examples

```
safedep subscription status
safedep subscription status --entitlements
safedep subscription status --entitlements --output json
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Status rendered. |
| non-zero | RPC error. |
