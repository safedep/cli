# safedep policy rule delete

Delete a package rule and publish the change at once as a new policy revision.
The rule stays in the history of the revisions.

## Synopsis

```
safedep policy rule delete RULE --reason TEXT [-m MESSAGE] [--yes]
```

## Flags

| Flag | Description |
|------|-------------|
| `--reason <text>` | Why the rule goes. Required. |
| `-m`, `--message <text>` | The publish message. |
| `-y`, `--yes` | Delete a block entry with no prompt. |

## Examples

```bash
safedep policy rule delete r_80b --reason "no longer needed"
```

```
✓ rule r_80b deleted · revision 51 published
```

The delete of a block entry loosens the policy, so the command asks first. With no
terminal, pass `--yes`. The delete of a trust entry does not ask.

```
Rule r_77e blocks pkg:npm/left-pad. The delete loosens the policy. Delete it? [y/N]:
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The rule was deleted and the revision was published. |
| non-zero | Validation or the request failed. |
