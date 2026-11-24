# safedep workload rule delete

Delete a workload trust rule by its id or its name. Every workload credential
that the rule gave stops working within 30 seconds, and the CI jobs that the
rule admits lose access to SafeDep Cloud.

## Synopsis

```
safedep workload rule delete RULE [--yes]
```

## Flags

| Flag | Description |
|------|-------------|
| `-y`, `--yes` | Delete the rule with no prompt. |

## Examples

```bash
safedep workload rule delete web
```

The command asks first. With no terminal, pass `--yes`.

```
The CI jobs that rule web admits, in acme/web, lose access to SafeDep Cloud. Delete it? [y/N]:
```

For a rule with no repository, the question says that the rule covers every
repository of the account:

```
Rule org covers every repository of acme. The CI jobs of every repository that it admits lose access to SafeDep Cloud. Delete it? [y/N]:
```

```
✓ rule wtr_9d1 deleted
Every workload credential that the rule gave stops working within 30 seconds
```

The exchanges of the rule stay in
[`safedep workload exchange list --rule`](./workload-exchange-list.md), by the
rule id.

## Permissions

Needs the Admin role. The server records an audit event.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The rule was deleted, or you answered no and nothing was deleted. |
| non-zero | The rule does not exist, or the request failed. |
