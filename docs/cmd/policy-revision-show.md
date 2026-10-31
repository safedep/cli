# safedep policy revision show

Show one policy revision: who published it, when, from which change, its change
direction, and the policy settings document at that revision.

## Synopsis

```
safedep policy revision show REVISION [--output table|plain|json]
```

## Examples

```bash
safedep policy revision show 47
```

```
Revision   47 · published 2d ago by alice · "payments: fail on medium, 7d cooldown (CUST-412)"
Source     CLI
Direction  TIGHTENS
Change     18 · safedep policy change show 18

Policy settings
base:
  scan_gate:
    fail_on: high
...
```

## Output

`json` holds `revision` and `policy`, the document in the form of
[`safedep policy get`](./policy-get.md).

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The revision was read. |
| non-zero | Authentication or the request failed. |
