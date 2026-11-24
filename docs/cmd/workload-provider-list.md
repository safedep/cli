# safedep workload provider list

List the workload identity providers of the tenant. A provider trusts the
identity tokens of one platform account, such as the GitHub Actions tokens of
the GitHub organization `acme`. A verified link of the SafeDep GitHub App
creates the provider of its account. No command creates a provider.

## Synopsis

```
safedep workload provider list [--limit N] [--page-token TOKEN] [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--limit <n>` | Page size. The server default applies when 0. |
| `--page-token <token>` | Continuation token from a prior response. |

## Examples

```bash
safedep workload provider list
```

```
Workload identity providers
PROVIDER             ID       OWNER ID  LINK    CREATED
github-actions:acme  wip_7a1  1001      active  2d ago
1 provider
```

`PROVIDER` is the `KIND:ACCOUNT` name that
[`safedep workload rule create --provider`](./workload-rule-create.md) takes.
`OWNER ID` is the immutable id of the account. A rule matches this id, never the
account name. A CI job gets a credential only through a provider whose `LINK` is
`active`. `suspended` means that GitHub suspended the installation of the
SafeDep GitHub App. `unlinked` means that the link is gone.

## Output

`json` holds `providers` (provider id, name, kind, account login, owner id, link
state, link id, creation time) and `next_page_token`. `plain` writes a
tab-separated header and one row for each provider.

## Permissions

Needs the Admin role.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was read. |
| non-zero | Authentication or the request failed. |
