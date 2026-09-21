# safedep integration bitbucket repository list

List the repositories of a linked Bitbucket workspace with their scan state.

## Synopsis

```
safedep integration bitbucket repository list [--link-id LINK_ID]
                                              [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--link-id <id>` | Bitbucket workspace link to list through. Resolved automatically when the tenant has exactly one link. |

Inherits root flags `--output` and `--profile`.

When `--link-id` is omitted, the CLI lists the tenant's workspace links and
uses the only one it finds. With no link, the command fails and points to
`link create`. With more than one link, it fails and lists the link IDs and
workspace slugs so you can pick one.

## Examples

```bash
safedep integration bitbucket repository list
```

```bash
safedep integration bitbucket repository list --link-id 01K88WX3G9RGAK8T3N5YJMFQN1 \
  --output json | jq -r '.repositories[] | select(.scan_state == "disabled") | .repository_uuid'
```

## Output

| Field | Description |
|-------|-------------|
| `link_id` | The workspace link the listing ran through, supplied or resolved. |
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
