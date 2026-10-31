# safedep policy rule list

List the active package rules at head, or at an older policy revision.

## Synopsis

```
safedep policy rule list [--effect trust|block] [--scope SCOPE] [--selector-prefix TEXT]
                         [--expiring DURATION] [--override NAME | --base] [--at-revision N]
                         [--limit N] [--page-token TOKEN] [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--effect <effect>` | `trust` or `block`. |
| `--scope <scope>` | Trust entries with this scope: `malware`, `cooldown` or `vulnerability`. |
| `--selector-prefix <text>` | Rules whose selector starts with this text, as `pkg:npm/`. |
| `--expiring <duration>` | Rules that expire within this time from now, as `7d`. |
| `--override <name>` | Rules that attach to this policy override. |
| `--base` | Rules that attach to the policy base. |
| `--at-revision <n>` | The rules that were active at this revision. |
| `--limit <n>` | Page size. The server default applies when 0. |
| `--page-token <token>` | Continuation token from a prior response. |

## Examples

Trust entries that expire this week:

```bash
safedep policy rule list --effect trust --expiring 7d
```

```
Package rules
RULE   SELECTOR         EFFECT  SCOPE             REASON          REQUESTED BY   ATTACHES TO  EXPIRES
r_77e  pkg:npm/@acme/x  trust   malware,cooldown  internal scope  platform-team  base         in 6d
r_80b  pkg:npm/leftpad  trust   malware           FP, S-71        web            base         in 2d
2 rules
```

## Output

`json` holds `rules` (rule id, purl, vers, effect, scopes, reason, ticket,
requester, override id, `attaches_to`, author, added and removed revision, times)
and `next_page_token`. `plain` writes a tab-separated header and one row for each
rule. The table humanizes the expiry.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was read. |
| non-zero | Validation, authentication or the request failed. |
