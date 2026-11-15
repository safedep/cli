# safedep policy revision list

List the policy revisions of the tenant, newest first.

## Synopsis

```
safedep policy revision list [--limit N] [--page-token TOKEN] [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--limit <n>` | Page size. The server default applies when 0. |
| `--page-token <token>` | Continuation token from a prior response. |

## Examples

```bash
safedep policy revision list --limit 5
```

```
Policy revisions
REV  PUBLISHED  BY     SOURCE  DIRECTION  MESSAGE
51   10m ago    alice  CLI     TIGHTENS   delete r_80b
50   12m ago    alice  CLI     LOOSENS    extend r_77e
49   1d ago     alice  CLI     LOOSENS    trust pkg:pypi/foo@1.0.3
48   1d ago     carol  CLI     TIGHTENS   block pkg:npm/evil (IR-2291)
47   2d ago     alice  CLI     LOOSENS    payments: fail on medium, 7d cooldown (CUST-412)
```

## Output

`json` holds `revisions` (revision, change id, author, message, source, change
direction, publish time) and `next_page_token`. `plain` writes a tab-separated
header and one row for each revision.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was read. |
| non-zero | Authentication or the request failed. |
