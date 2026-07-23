# safedep subscription portal open

## Synopsis

```
safedep subscription portal open
```

## Description

`subscription portal open` opens the provider-hosted billing portal in your browser,
where you can manage payment methods and invoices and cancel the subscription. The
CLI does not surface these low-level billing operations directly; they are delegated
to the portal. The portal URL is always printed so it can be opened manually in a
headless session.

## Examples

```
safedep subscription portal open
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Portal session created and opened (URL printed). |
| non-zero | No billing customer, or RPC error. |
