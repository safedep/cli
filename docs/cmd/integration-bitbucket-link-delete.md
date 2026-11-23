# safedep integration bitbucket link delete

Unlink a Bitbucket workspace from the active SafeDep tenant.

## Synopsis

```
safedep integration bitbucket link delete [--account WORKSPACE]
```

## Description

SafeDep revokes the workspace link and discards the stored app token. After
the unlink, SafeDep no longer scans the workspace's repositories.

`--account` names the workspace by its slug. Without it, the command resolves
the tenant's only active link. The command picks from active links only, so a
repeat call after the delete fails with not found.

`--link-id <id>` still selects a link by its internal ID. It is hidden from
the help and stays for scripts. With `--link-id` alone, the command lists no
links and is idempotent: deleting a link that is already revoked succeeds.
With `--link-id` and `--account`, both must name the same active link.

The SafeDep Forge app stays installed in the workspace. It keeps sending
events, and SafeDep ignores them. Only a Bitbucket workspace admin can
uninstall the app. Uninstall it in Bitbucket to stop that traffic.

To link the workspace again, run
`safedep integration bitbucket link create` and redeem the new code in the
workspace's SafeDep settings page. No reinstall is needed.

## Flags

| Flag | Description |
|------|-------------|
| `--account <workspace>` | Slug of the linked Bitbucket workspace to unlink, for example `safedep`. Matched without regard to case. Needed when the tenant has more than one linked workspace. Find slugs with `safedep integration bitbucket link list`. |

## Examples

```bash
safedep integration bitbucket link delete
```

```bash
safedep integration bitbucket link delete --account safedep
```

## Authentication

Requires a control-plane OAuth session with the admin role. Run
`safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The link is revoked. With `--link-id` alone, this includes a link revoked before this call. |
| non-zero | Authentication failed, the tenant has no link, more than one link without `--account`, or no active link for the account, the link does not belong to the tenant, or the request failed. |
