# safedep project list

List the projects of the tenant with their labels. A repository is a project.

## Synopsis

```
safedep project list [--selector SELECTOR] [--source SOURCE]... [--name NAME]...
                     [--limit N] [--page-token TOKEN] [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--selector <selector>` | A label selector, as a policy override uses. See below. |
| `--source <source>` | `github`, `gitlab`, `bitbucket`, `other`, `developer-machine` or `container-image`. Repeatable. |
| `--name <name>` | An exact project name. Repeat for up to 10 names. |
| `--limit <n>` | Page size. The server default applies when 0. |
| `--page-token <token>` | Continuation token from a prior response. |

A selector has terms separated by commas. The terms AND together:

| Term | Matches |
|------|---------|
| `key=value` | The label has the value. |
| `key!=value` | The label does not have the value. |
| `key in (a,b)` | The label has one of the values. |
| `key notin (a,b)` | The label has none of the values. |
| `key` | The label exists. |
| `!key` | The label does not exist. |

## Examples

Find the projects that a policy override will select:

```bash
safedep project list --selector scm-property/criticality=high
```

```
Projects
PROJECT            SOURCE  LABELS                                          VULNS  VIOLATIONS  CREATED
acme/payments-api  github  custom/team=pay, scm-property/criticality=high  3      -           3d ago
acme/ledger        github  scm-property/criticality=high                   0      -           1mo ago
2 projects
```

## Output

`json` holds `projects` (project id, name, source, origin URL, labels, counts,
creation time) and `next_page_token`. `plain` writes a tab-separated header and
one row for each project.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was read. |
| non-zero | Validation, authentication or the request failed. |
