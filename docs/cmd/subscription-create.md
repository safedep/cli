# safedep subscription create

## Synopsis

```
safedep subscription create [--seats N] [customer flags] [--wait] [--timeout DUR]
```

## Description

`subscription create` subscribes the tenant account to the Professional plan. If no
billing profile exists, it is created first (interactive on a terminal, flags
otherwise). The command opens the provider-hosted checkout page in your browser and,
by default, waits until the subscription becomes active. Enterprise plans are custom
- contact sales.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--seats` | `5` | Number of seats (minimum 1). |
| `--wait` | `true` | Wait for the subscription to become active after checkout. |
| `--timeout` | `10m` | Maximum time to wait. |
| customer flags | - | Billing profile fields, used when no customer exists. |

## Examples

```
safedep subscription create
safedep subscription create --seats 10
safedep subscription create --seats 10 --wait=false
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Subscribed, already subscribed, or checkout opened with `--wait=false`. |
| non-zero | Checkout error, missing billing details in a non-interactive session, or RPC error. |
