# safedep integration bitbucket link delete

Unlink a Bitbucket workspace from the active SafeDep tenant.

## Synopsis

```
safedep integration bitbucket link delete [--link-id <id>]
```

## Description

SafeDep revokes the workspace link and discards the stored app token. After
the unlink, SafeDep no longer scans the workspace's repositories.

With `--link-id`, the command is idempotent: deleting a link that is already
revoked succeeds. Without `--link-id`, the command resolves the tenant's only
active link. After that link is deleted, a repeat call without `--link-id`
finds no link and fails with not found.

The SafeDep Forge app stays installed in the workspace. It keeps sending
events, and SafeDep ignores them. Only a Bitbucket workspace admin can
uninstall the app. Uninstall it in Bitbucket to stop that traffic.

To link the workspace again, run
`safedep integration bitbucket link create` and redeem the new code in the
workspace's SafeDep settings page. No reinstall is needed.

## Flags

| Flag | Description |
|------|-------------|
| `--link-id` | The workspace link to delete. When the tenant has exactly one link, the command resolves it automatically. Find link IDs with `safedep integration bitbucket link list`. |

## Examples

```bash
safedep integration bitbucket link delete
```

```bash
safedep integration bitbucket link delete --link-id 01J8X2...
```

## Authentication

Requires a control-plane OAuth session with the admin role. Run
`safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The link is revoked. With `--link-id`, this includes a link revoked before this call. |
| non-zero | Authentication failed, the tenant has no link or more than one link without `--link-id`, the link does not belong to the tenant, or the request failed. |
