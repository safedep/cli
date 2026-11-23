# safedep workload rule delete

Delete a workload trust rule by its id or its name. Every workload credential
that the rule gave stops working within 30 seconds, and the CI jobs that the
rule covered get no new credential.

## Synopsis

```
safedep workload rule delete RULE
```

## Examples

```bash
safedep workload rule delete web
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
| `0` | The rule was deleted. |
| non-zero | The rule does not exist, or the request failed. |
