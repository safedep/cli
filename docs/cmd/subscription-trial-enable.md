# safedep subscription trial enable

## Synopsis

```
safedep subscription trial enable [customer flags] [--wait] [--timeout DUR]
```

## Description

`subscription trial enable` activates the free trial for the tenant account. If no
billing profile exists yet, it is created first: interactively on a terminal, or
from the customer flags in agent/CI environments. Trial activation syncs through the
billing provider, so by default the command waits until the account reaches the
trial state.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--wait` | `true` | Wait for the trial to become active. |
| `--timeout` | `2m` | Maximum time to wait. |
| `--company`, `--phone`, `--country`, `--state`, `--city`, `--postal`, `--line1`, `--line2`, `--tax-id` | - | Billing profile fields, used when no customer exists (see `subscription customer create`). |

## Examples

```
safedep subscription trial enable
safedep subscription trial enable --company "Acme Inc" --phone "+14155550100" \
  --country US --state CA --city "San Francisco" --postal 94105 --line1 "500 Howard St"
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Trial activated (or already active). |
| non-zero | Ineligible, missing billing details in a non-interactive session, or RPC error. |
