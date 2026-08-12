# safedep subscription pricing

## Synopsis

```
safedep subscription pricing
```

## Description

`subscription pricing` shows the published list prices for plans and add-ons. The
prices are the same for every tenant, so they do not include a tenant discount or
the exact amount charged at purchase. Each product lists its price per cadence
(`Monthly`, `Yearly`) or `Per unit` for a metered (usage-based) price.

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
