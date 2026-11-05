# safedep policy change list

List the policy changes of the tenant, newest first.

## Synopsis

```
safedep policy change list [--state open|published|deleted]
                           [--limit N] [--page-token TOKEN]
                           [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--state <state>` | Filter: `open`, `published` or `deleted`. |
| `--limit <n>` | Page size. The server default applies when 0. |
| `--page-token <token>` | Continuation token from a prior response. |

Inherits root flags `--output` and `--profile`.

## Examples

```bash
safedep policy change list --state open
```

```
Policy changes
CHANGE  TITLE                        STATE  BASE REV  EDITS     AUTHOR  UPDATED
18      payments: medium + cooldown  open   46        settings  alice   2m ago
```

## Output

`json` holds `changes` (id, title, state, base and published revision, author,
what the change edits, times) and `next_page_token`. `plain` writes a
tab-separated header and one row for each change.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was read, also when it is empty. |
| non-zero | Validation, authentication or the request failed. |
