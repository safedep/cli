# safedep policy change create

Create a policy change that restores capabilities from an old policy revision,
for a rollback.

## Synopsis

```
safedep policy change create --from-revision N [--capability NAME]...
                             [--package-rules head|revision] [-m TITLE]
```

## Flags

| Flag | Description |
|------|-------------|
| `--from-revision <n>` | The revision to restore from. Required. |
| `--capability <name>` | A capability to restore, as in `scan_gate` or `package_admission`. Repeatable. Every capability when absent. |
| `--package-rules <mode>` | `head` (default) keeps the package rules at head. `revision` restores the set of `--from-revision`. |
| `-m`, `--message <title>` | The title of the change. |

For each restored capability, the policy base and every policy override take
their values from that revision. The change is a draft. Revision numbers only go
up, so its publish makes a new revision.

## Examples

```bash
safedep policy change create --from-revision 46 --capability scan_gate -m "revert fail_on to high"
```

```
✓ change 20 restores scan_gate from revision 46 · package rules stay at head
! It loosens scan_gate.fail_on from medium to high in override "payments"
  Review it: safedep policy change show 20
  Publish it: safedep policy change publish 20
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The change was created. |
| non-zero | Validation or the request failed. |
