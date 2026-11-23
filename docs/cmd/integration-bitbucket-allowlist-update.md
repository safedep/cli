# safedep integration bitbucket allowlist update

Change which repositories of a linked Bitbucket workspace SafeDep scans.

## Synopsis

```
safedep integration bitbucket allowlist update [--account WORKSPACE]
                                               [--scope all|selected]
                                               [--enable REPOSITORY_UUID]...
                                               [--disable REPOSITORY_UUID]...
                                               [--output table|plain|json]
```

## Description

A linked workspace has a scan scope. Scope `all` scans every repository in
the workspace, including repositories created later. Scope `selected` scans
only the repositories on the scan allowlist. A fresh link starts with scope
`selected` and an empty allowlist, so nothing is scanned until this command
opens the scope or fills the allowlist.

## Flags

| Flag | Description |
|------|-------------|
| `--account <workspace>` | Slug of the linked Bitbucket workspace to update, for example `safedep`. Matched without regard to case. Needed when the tenant has more than one linked workspace. |
| `--scope <all\|selected>` | Scan scope after the update. Omit to keep the stored scope. Switching scope does not change the stored allowlist. |
| `--enable <uuid>` | Repository UUID to add to the allowlist. Braced and uppercase forms are accepted and normalized. Repeat for multiple repositories. A UUID already on the allowlist is a no-op. |
| `--disable <uuid>` | Repository UUID to remove from the allowlist. Braced and uppercase forms are accepted and normalized. Repeat for multiple repositories. A UUID not on the allowlist is a no-op. |

Pass at least one of `--scope`, `--enable`, or `--disable`. Scope `all`
takes no `--enable` or `--disable` values. At most 500 UUIDs per flag per
request: a larger allowlist grows over several calls. Get repository UUIDs
from `safedep integration bitbucket repository list`.

`--link-id <id>` still selects a link by its internal ID. It is hidden from
the help and stays for scripts. With `--link-id` alone, the command lists no
links. With `--link-id` and `--account`, both must name the same link.

## Examples

Scan every repository in the workspace:

```bash
safedep integration bitbucket allowlist update --account safedep --scope all
```

Scan two selected repositories only:

```bash
safedep integration bitbucket allowlist update --scope selected \
  --enable 9a2b3c4d-0000-0000-0000-000000000001 \
  --enable 9a2b3c4d-0000-0000-0000-000000000002
```

Remove one repository from the allowlist:

```bash
safedep integration bitbucket allowlist update \
  --disable 9a2b3c4d-0000-0000-0000-000000000001
```

## Output

| Field | Description |
|-------|-------------|
| `link_id` | The workspace link the update ran against, supplied or resolved. |
| `account` | The workspace slug of that link. Omitted when only `--link-id` named the link. In `plain` output, the last column, with `-` for no slug. |
| `scan_scope` | The scan scope after the update. |
| `allowlisted_repository_count` | The number of repositories on the allowlist after the update. |

`table` shows the account in place of the link ID, or `link <id>` when only
`--link-id` named the link.

## Authentication

Requires a control-plane OAuth session with the tenant admin role. Run
`safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The update applied. |
| non-zero | Local validation, link resolution, authentication, or the request failed. |
