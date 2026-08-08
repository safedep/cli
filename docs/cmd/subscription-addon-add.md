# safedep subscription addon add

## Synopsis

```
safedep subscription addon add <add-on> --accept-terms
```

## Description

`subscription addon add` buys an add-on on the tenant account's active paid
subscription. The add-on is billed on the plan's cadence (monthly or yearly) and
charged immediately against the payment method on file, so it requires explicit
acceptance of the add-on terms (https://safedep.io/terms/) via `--accept-terms`.

If prerequisites are not met, the command routes you to the next step: subscribe
first (`subscription create`), settle a past-due balance, or add a payment method
(`subscription portal open`).

The purchase is synchronous, but the entitlement activates once the provider
webhook syncs the subscription. By default the command waits until the add-on
shows on the account. Pass `--wait=false` to return as soon as the purchase is
accepted.

Available add-ons: `threat-intel-feed`.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--accept-terms` | `false` | Accept the add-on billing terms. Required to buy. |
| `--wait` | `true` | Wait for the add-on to activate on the subscription. |
| `--timeout` | `5m` | Maximum time to wait for activation. |

## Examples

```
safedep subscription addon add threat-intel-feed --accept-terms
```

```
# Buy without waiting for the webhook to sync.
safedep subscription addon add threat-intel-feed --accept-terms --wait=false
```
