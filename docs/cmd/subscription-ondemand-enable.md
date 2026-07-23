# safedep subscription ondemand enable

## Synopsis

```
safedep subscription ondemand enable --accept-terms
```

## Description

`subscription ondemand enable` opts the tenant account in to usage-based overage
billing beyond the included seat allowance. It requires an active paid (non-trial)
subscription with a payment method on file, and explicit acceptance of the on-demand
terms (https://safedep.io/terms/) via `--accept-terms`.

If prerequisites are not met, the command routes you to the next step: subscribe
first (`subscription create`) or add a payment method (`subscription portal`).

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--accept-terms` | `false` | Accept the on-demand billing terms. Required to enable. |

## Examples

```
safedep subscription ondemand enable --accept-terms
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | On-demand billing enabled. |
| non-zero | Terms not accepted, no paid plan, no payment method, or RPC error. |
