# safedep subscription addon remove

## Synopsis

```
safedep subscription addon remove <add-on>
```

## Description

`subscription addon remove` removes an add-on from the tenant account's
subscription. The provider credits the unused portion on the next invoice.

By default the command waits until the add-on clears from the account, which
happens once the provider webhook syncs the subscription. Pass `--wait=false` to
return as soon as the removal is accepted.

Available add-ons: `threat-intel-feed`.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--wait` | `true` | Wait for the add-on to clear from the subscription. |
| `--timeout` | `5m` | Maximum time to wait. |

## Examples

```
safedep subscription addon remove threat-intel-feed
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Add-on removed (and cleared, unless `--wait=false`). |
| non-zero | Unknown add-on, wait timeout, or RPC error. |
