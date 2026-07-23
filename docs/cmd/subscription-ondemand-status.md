# safedep subscription ondemand status

## Synopsis

```
safedep subscription ondemand status
```

## Description

`subscription ondemand status` shows the tenant account's on-demand billing state:
whether it is enabled, whether a payment method is on file, and the payment (dunning)
posture.

## Examples

```
safedep subscription ondemand status
safedep subscription ondemand status --output json
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | State rendered. |
| non-zero | RPC error. |
