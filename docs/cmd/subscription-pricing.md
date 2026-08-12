# safedep subscription pricing

## Synopsis

```
safedep subscription pricing
```

## Description

`subscription pricing` shows the published list prices for plans and add-ons. The
prices are the same for every tenant, so they do not include a tenant discount or
the exact amount charged at purchase. Each product lists its price per cadence
(`Monthly`, `Yearly`) and shows the selling unit where one applies: a seat-priced
plan reads `$20.00 per seat`, and a metered (usage-based) price reads `Per scan`.

## Flags

This command takes no flags.

## Examples

```
safedep subscription pricing
safedep subscription pricing --output json
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Pricing rendered. |
| non-zero | RPC error. |
