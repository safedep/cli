# safedep subscription customer create

## Synopsis

```
safedep subscription customer create [--company ... --phone ... --country ... ...]
```

## Description

`subscription customer create` creates the billing customer profile for the tenant
account. It prompts interactively on a terminal; in agent/CI environments the fields
must be supplied as flags. The billing customer is a prerequisite for starting a
trial or subscribing.

## Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--company` | yes | Company or customer name. |
| `--phone` | yes | Contact phone number. |
| `--country` | yes | Billing country (ISO 3166-1 alpha-2). |
| `--state` | yes | Billing state/region (ISO 3166-2). |
| `--city` | yes | Billing city. |
| `--postal` | yes | Billing postal code. |
| `--line1` | yes | Billing address line 1. |
| `--line2` | no | Billing address line 2. |
| `--tax-id` | no | Tax ID. |

## Examples

```
safedep subscription customer create
safedep subscription customer create --company "Acme Inc" --phone "+14155550100" \
  --country US --state CA --city "San Francisco" --postal 94105 --line1 "500 Howard St"
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Customer created. |
| non-zero | Customer already exists, missing fields in a non-interactive session, provider error, or RPC error. |
