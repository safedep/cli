# safedep policy rule update

Change the reason, expiry, ticket or requester of a package rule, and publish the
change at once as a new policy revision. A rule never changes its selector,
effect or trust scopes: delete it and create another.

## Synopsis

```
safedep policy rule update RULE [--reason TEXT] [--expires WHEN] [--ticket REF]
                                [--requested-by WHO] [-m MESSAGE]
```

## Flags

| Flag | Description |
|------|-------------|
| `--reason <text>` | The new reason. |
| `--expires <when>` | The new expiry: a duration from now, as `90d`, or a date. |
| `--ticket <ref>` | The new ticket. An empty value clears it. |
| `--requested-by <who>` | The new requester. An empty value clears it. |
| `-m`, `--message <text>` | The publish message. |

Give at least one of the fields. A later expiry of a trust entry loosens the policy. A trust entry expires at most 90 days from now. The command reads the rule to check it before the request.

## Examples

```bash
safedep policy rule update r_77e --expires 90d --reason "internal scope, renewed for Q1"
```

```
✓ rule r_77e now expires on 2027-01-29 · revision 50 published
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The rule was updated and the revision was published. |
| non-zero | Validation or the request failed. |
