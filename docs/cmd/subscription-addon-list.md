# safedep subscription addon list

## Synopsis

```
safedep subscription addon list
```

## Description

`subscription addon list` lists the add-ons purchased on the tenant account's
subscription. Add-ons grant extra features on top of the paid plan and are billed
on the plan's cadence.

Add-ons reflect purchased items only. An add-on granted through an entitlement
without a purchase (for example a comped plan) does not show here. List those with
`safedep subscription status --entitlements`.

## Flags

This command takes no flags.

## Examples

```
safedep subscription addon list
```

```
# Script against the JSON output.
safedep subscription addon list --output json
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Add-ons listed (including an empty list). |
| non-zero | RPC error. |
