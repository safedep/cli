# safedep project sync

Materialize SafeDep projects from GitHub repositories reachable through a linked
GitHub App installation.

## Synopsis

```
safedep project sync [OWNER/REPOSITORY...] [--repository-id ID]
                     [--link-id LINK_ID] [--output table|plain|json]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `OWNER/REPOSITORY` | GitHub repository name, for example `safedep/cli`. |

Supply between 1 and 100 repositories in total across positional names and
`--repository-id` values. Repository names are matched case-insensitively, the
way GitHub treats them.

A SafeDep project's canonical identity is the tenant, the GitHub source, and the
immutable GitHub repository ID. Owner and repository names are metadata, so the
CLI resolves every name to a repository ID before sending the request. Syncing
is idempotent: repeating it for the same repository returns the same project ID.

## Flags

| Flag | Description |
|------|-------------|
| `--repository-id <id>` | Immutable GitHub repository ID to sync instead of a name. Repeat the flag, or pass a comma-separated list, to select multiple repositories. |
| `--link-id <id>` | GitHub App installation link to sync through. Resolved automatically when the tenant has exactly one link. |

Inherits root flags `--output` and `--profile`.

## Installation links

A link associates a GitHub App installation with a SafeDep tenant, and it is the
access path used to resolve repositories. It is not the identity of the resulting
projects.

When `--link-id` is omitted, the CLI lists the tenant's links and uses the only
one it finds. With no link, the command fails and asks you to install and link
the SafeDep GitHub App. With more than one link, it fails and lists the link IDs
and their GitHub account logins so you can pick one with `--link-id`.

## Examples

Sync one repository:

```bash
safedep project sync safedep/cli
```

Sync several repositories in one request:

```bash
safedep project sync safedep/cli safedep/vet safedep/pmg
```

Sync by immutable repository ID, skipping name resolution:

```bash
safedep project sync --repository-id 1296269 --repository-id 10270250
```

Choose the installation link explicitly:

```bash
safedep project sync safedep/cli --link-id 01K88WX3G9RGAK8T3N5YJMFQN1
```

Sync and scan the resulting projects:

```bash
safedep project sync safedep/cli --output json \
  | jq -r '.projects[].project_id' \
  | xargs -I{} safedep project scan create --project-id {}
```

## Output

| Field | Description |
|-------|-------------|
| `link_id` | The installation link the sync ran through, whether supplied or resolved. Reported once per invocation, in the JSON body and the table footer. |
| `repository_id` | The immutable GitHub repository ID. |
| `repository_name` | The `owner/repository` name as GitHub reports it. Empty for repositories selected by ID, which the CLI never resolved. |
| `project_id` | The stable SafeDep project ID for that repository. |

An unresolved repository name renders as `-` in table and plain output and is
omitted from JSON.

JSON shape:

```json
{
  "link_id": "link-id",
  "projects": [
    {
      "repository_id": 1296269,
      "repository_name": "safedep/cli",
      "project_id": "project-id"
    }
  ]
}
```

`table` renders one row per synced repository and reports the link ID in the
footer. `plain` renders a tab-separated header and one tab-separated row per
synced repository, so every row has the same field count and can be cut by
column. The link ID is not part of plain output: read it from `table` or `json`.

## Resolution and failure behaviour

The CLI validates the batch size, the `OWNER/REPOSITORY` shape, and duplicate
selectors locally, then resolves the installation link and every repository name
before it sends the sync request. A name that the installation cannot reach fails
the command without materializing any project, and the error names the
repository so you can grant it to the installation or retry with
`--repository-id`.

After resolution, the request carries `--repository-id` values first, followed by
resolved names in argument order. A name that resolves to an already selected
repository ID is rejected locally.

Control Tower validates every repository against the link before it writes, so a
failed request materializes no projects. Installation, repository access, and
suspension failures preserve their gRPC status details internally. The CLI prints
a concise message, a stable error code, and recovery guidance instead of raw
nested gRPC text.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Every requested repository has a SafeDep project. |
| non-zero | Local validation, link or repository resolution, authentication, or the sync request failed. |
