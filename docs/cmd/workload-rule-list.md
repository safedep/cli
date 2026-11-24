# safedep workload rule list

List the workload trust rules of the tenant, oldest first.

## Synopsis

```
safedep workload rule list [--provider PROVIDER] [--limit N] [--page-token TOKEN]
                           [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--provider <provider>` | Only the rules of this provider: a provider id or `KIND:ACCOUNT`, as in `github-actions:acme`. |
| `--limit <n>` | Page size. The server default applies when 0. |
| `--page-token <token>` | Continuation token from a prior response. |

## Examples

```bash
safedep workload rule list
```

```
Workload trust rules
RULE     NAME  PROVIDER             GRANT  REPOSITORIES              CONDITIONS                        LAST USED
wtr_4c2  org   github-actions:acme  tool   every repository of acme  -                                 3m ago
wtr_9d1  web   github-actions:acme  tool   acme/web                  ref matches refs/heads/release/*  never
2 rules
```

`CONDITIONS` leaves out the repositories, which `REPOSITORIES` shows.

## Output

`json` holds `rules` (each as [`safedep workload rule show`](./workload-rule-show.md)
writes it) and `next_page_token`. `plain` writes a tab-separated header and one
row for each rule. Its `repositories` value has the form `acme/web=123456`.

## Permissions

Needs the Admin role.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was read. |
| non-zero | The provider is unknown, or the request failed. |
