# safedep integration bitbucket repository list

List the repositories of a linked Bitbucket workspace with their scan state.

## Synopsis

```
safedep integration bitbucket repository list [--account WORKSPACE]
                                              [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--account <workspace>` | Slug of the linked Bitbucket workspace to list, for example `safedep`. Matched without regard to case. Needed when the tenant has more than one linked workspace. |

Inherits root flags `--output` and `--profile`.

When `--account` is omitted, the CLI lists the tenant's workspace links and
uses the only one it finds. With no link, the command fails and points to
`link create`. With more than one link, it fails and lists the workspace
slugs so you can pick one with `--account`.

`--link-id <id>` still selects a link by its internal ID. It is hidden from
the help and stays for scripts. With `--link-id` alone, the command lists no
links. With `--link-id` and `--account`, both must name the same link.

## Examples

```bash
safedep integration bitbucket repository list
```

```bash
safedep integration bitbucket repository list --account safedep \
  --output json | jq -r '.repositories[] | select(.scan_state == "disabled") | .repository_uuid'
```

## Output

| Field | Description |
|-------|-------------|
| `link_id` | The workspace link the listing ran through, supplied or resolved. |
| `account` | The workspace slug of that link. Omitted when only `--link-id` named the link. |
| `scan_scope` | The workspace's scan scope: `all` or `selected`. |
| `repository_uuid` | The immutable Bitbucket repository UUID. Feed it to `allowlist update`. |
| `full_name` | The `workspace/repository` name. |
| `visibility` | `public` or `private`. |
| `default_branch` | The repository's default branch. |
| `scan_state` | Whether SafeDep scans the repository under the current scope: `enabled` or `disabled`. |

The command walks every page of the listing.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The listing completed. |
| non-zero | Link resolution, authentication, or the request failed. |
