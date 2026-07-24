# safedep subscription customer show

## Synopsis

```
safedep subscription customer show
```

## Description

`subscription customer show` displays the billing customer profile linked to the
tenant account.

## Examples

```
safedep subscription customer show
safedep subscription customer show --output json
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Customer rendered. |
| non-zero | No customer linked, or RPC error. |
