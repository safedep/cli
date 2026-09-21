# safedep project sync

Materialize SafeDep projects from repositories reachable through a linked
source integration: a GitHub App installation or a Bitbucket workspace.

## Synopsis

```
safedep project sync [OWNER/REPOSITORY...] [--source github|bitbucket]
                     [--repository-id ID] [--repository-uuid UUID]
                     [--link-id LINK_ID] [--output table|plain|json]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `OWNER/REPOSITORY` | Repository name in its source, for example `safedep/cli` on GitHub or `safedep/vet-pipe` on Bitbucket. |

Supply between 1 and 100 repositories in total across positional names,
`--repository-id`, and `--repository-uuid` values. Repository names are
matched case-insensitively.

A SafeDep project's canonical identity is the tenant, the source, and the
source's immutable repository identity: the GitHub repository ID or the
Bitbucket repository UUID. Names are metadata, so the CLI resolves every name
to that identity before sending the request. Syncing is idempotent: repeating
it for the same repository returns the same project ID.

## Sources

`--source` picks the repository source. When omitted:

1. `--repository-id` implies `github` and `--repository-uuid` implies
   `bitbucket`.
2. Otherwise the CLI uses the one source the tenant has links for. A tenant
   with links to both sources must pass `--source`. A tenant with no link
   fails with guidance.

One invocation serves one source, so `--repository-id` and
`--repository-uuid` do not combine.

## Flags

| Flag | Description |
|------|-------------|
| `--source <github\|bitbucket>` | Repository source. See Sources for the default. |
| `--repository-id <id>` | Immutable GitHub repository ID to sync instead of a name. Repeat the flag, or pass a comma-separated list, to select multiple repositories. |
| `--repository-uuid <uuid>` | Immutable Bitbucket repository UUID to sync instead of a name. Braced and uppercase forms are accepted and normalized. Repeat for multiple repositories. Get UUIDs from `safedep integration bitbucket repository list`. |
| `--link-id <id>` | Source link to sync through. Resolved automatically when the tenant has exactly one link for the source. |

Inherits root flags `--output` and `--profile`.

## Source links

A link associates a GitHub App installation or a Bitbucket workspace with a
SafeDep tenant, and it is the access path used to resolve repositories. It is
not the identity of the resulting projects.

When `--link-id` is omitted, the CLI lists the tenant's links for the source
and uses the only one it finds. With no link, the command fails and asks you
to install and link the source integration. With more than one link, it fails
and lists the link IDs so you can pick one with `--link-id`.

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

Sync a Bitbucket repository by name, or by UUID:

```bash
safedep project sync safedep/vet-pipe --source bitbucket
safedep project sync --repository-uuid d6c2f02b-7f78-49a8-b7e3-cf8b86efd115
```

Choose the source link explicitly:

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
| `link_id` | The source link the sync ran through, whether supplied or resolved. Reported once per invocation, in the JSON body and the table footer. |
| `repository_id` | GitHub only: the immutable repository ID. |
| `repository_uuid` | Bitbucket only: the immutable repository UUID. |
| `repository_name` | The repository name as the source reports it. Empty for repositories selected by ID or UUID, which the CLI never resolved. |
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
selectors locally, then resolves the source link and every repository name
before it sends the sync request. A name that the source link cannot reach fails
the command without materializing any project, and the error names the
repository. Grant it to the GitHub App installation and retry, or retry with the
immutable selector: `--repository-id` for GitHub, `--repository-uuid` for
Bitbucket. Both immutable selectors skip name resolution.

After resolution, the request carries the immutable selector values first,
followed by resolved names in argument order. A name that resolves to an
already selected repository is rejected locally.

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
