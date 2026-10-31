# safedep policy change publish

Publish an open policy change as a new policy revision for the whole tenant.

## Synopsis

```
safedep policy change publish CHANGE [-m MESSAGE]
```

## Flags

| Flag | Description |
|------|-------------|
| `-m`, `--message <text>` | The publish message. |

## Examples

```bash
safedep policy change publish 18 -m "payments: fail on medium, 7d cooldown (CUST-412)"
```

```
✓ revision 47 published
  Projects resolve it at their next check. Endpoints apply it after their next policy fetch
  Endpoints that have not applied it yet: safedep endpoint list --applied-revision '<47'
```

When a part that the change edits moved since its base revision, the publish stops:

```
✗ The policy moved: a part that change 19 edits changed after its base revision
  Sync it and review it again: safedep policy change sync 19
```

In the MVP every publish needs the Admin role. A change that loosens the policy
sends a notice to every Admin and Owner of the tenant.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The revision was published. |
| non-zero | The change is not open, it is stale, or the request failed. |
