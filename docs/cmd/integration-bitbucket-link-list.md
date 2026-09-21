# safedep integration bitbucket link list

List the Bitbucket workspaces linked to the active SafeDep tenant.

## Synopsis

```
safedep integration bitbucket link list [--output table|plain|json]
```

## Description

Each row is one linked workspace with its link ID. The link ID feeds
`safedep integration bitbucket repository list` and
`safedep integration bitbucket allowlist update`. The command walks every
page of the listing.

## Examples

```bash
safedep integration bitbucket link list
```

```bash
safedep integration bitbucket link list --output json | jq -r '.links[].link_id'
```

## Output

| Field | Description |
|-------|-------------|
| `link_id` | The tenant-owned workspace link ID. |
| `workspace_uuid` | The immutable Bitbucket workspace UUID: lowercase, hyphenated, no braces. |
| `workspace_slug` | The workspace slug at the last sync. Mutable metadata, not identity. |
| `workspace_name` | The workspace display name at the last sync. Mutable metadata. |

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The listing completed, with zero or more links. |
| non-zero | Authentication or the request failed. |
