# safedep workload exchange list

List the workload exchanges of the tenant, newest first. Each exchange is one
request of a CI job for a credential: SafeDep issued it or denied it. The server
keeps an exchange for 30 days.

## Synopsis

```
safedep workload exchange list [--denied] [--since DURATION] [--rule RULE]
                               [--limit N] [--page-token TOKEN] [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--denied` | Only the exchanges that the server denied. |
| `--since <duration>` | Only exchanges in this time before now, as `7d` or `12h`. |
| `--rule <rule>` | Only exchanges that this rule matched: a rule id, or the name of a live rule. The id of a deleted rule also works. |
| `--limit <n>` | Page size. The server default applies when 0. |
| `--page-token <token>` | Continuation token from a prior response. |

## Examples

A CI job got no credential. Find out why:

```bash
safedep workload exchange list --denied --since 1d
```

```
Workload exchanges
WHEN    OUTCOME  WORKLOAD                                        RULE  DETAIL
5m ago  denied   github:acme/web@refs/heads/feature-x run 987/1  -     no workload trust rule gives TOOL to acme/web on refs/heads/feature-x
1 exchange
```

The reason names the missing rule. Create a rule that covers the job with
[`safedep workload rule create`](./workload-rule-create.md). For an issued
exchange, `DETAIL` says when the credential expires, or when it was revoked.

When `--rule` names no live rule, the CLI sends the value as a rule id. If no
exchange matches, the command says that no live rule has that name or id. To list
the exchanges of a deleted rule, pass its rule id.

## Output

`json` holds `exchanges` (exchange id, outcome, denial reason, rule id, provider
id, grant, display identity, the kept claims, endpoint id, and the times of the
exchange, the expiry and the revocation) and `next_page_token`. `plain` writes a
tab-separated header and one row for each exchange.

## Permissions

Needs the Admin role.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was read. |
| non-zero | Validation or the request failed. |
