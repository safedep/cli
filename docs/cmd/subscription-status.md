# safedep subscription status

## Synopsis

```
safedep subscription status
```

## Description

`subscription status` shows the tenant account's plan state: subscription status
(`FREE`, `ACTIVE_TRIAL`, `ACTIVE`, `PAST_DUE`, `ACTIVE_PENDING_CANCELLATION`), tier,
trial days remaining when in a trial, on-demand billing state, and entitlements.
It is the hub command; other subscription commands point back to it.

## Flags

Only the global `--output` flag.

## Examples

```
safedep subscription status
safedep subscription status --output json
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Status rendered. |
| non-zero | RPC error. |
