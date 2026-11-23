# safedep project sync

Materialize SafeDep projects from repositories reachable through a linked
source integration: a GitHub App installation or a Bitbucket workspace.

## Synopsis

```
safedep project sync [OWNER/REPOSITORY...] [--source github|bitbucket]
                     [--account ACCOUNT]
                     [--repository-id ID] [--repository-uuid UUID]
                     [--output table|plain|json]
safedep project sync --account ACCOUNT --all [--source github|bitbucket]
                     [--output table|plain|json]
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

## Accounts

An account is the GitHub organization or user of a GitHub App installation,
or the slug of a Bitbucket workspace. Each source link of the tenant has one
account. Accounts match without regard to case.

The OWNER of each name picks the link whose account has that name. The match
also picks the source, so `safedep project sync safedep/vet acme/app` needs
neither `--source` nor `--account`. Names with two owners make one sync
request per link. The CLI resolves the names of every link before it sends
the first request, so a name that no link can reach syncs nothing.

- When no link has the owner's account and the tenant has exactly one link
  of the allowed sources, that link serves the name. This covers a link whose
  account name is missing, or stale after a rename.
- When no link has the owner's account and the tenant has more links, the
  command fails and lists the linked accounts.
- When the owner names a GitHub account and a Bitbucket workspace, the
  command fails and asks for `--source`.
- One invocation serves one source. Names whose owners resolve to GitHub and
  to Bitbucket fail, so sync each source in its own command.

`--account` picks the link and every name resolves through it. You need it
with `--all`, and with `--repository-id` or `--repository-uuid` when the
tenant has more than one link of that source. Those selectors carry no owner.

`--link-id <id>` still selects a link by its internal ID. It is hidden from
the help and stays for scripts. With `--link-id` and a known source, the
command lists no links. With `--link-id` and `--account`, both must name the
same link.

## Sources

`--source` picks the repository source. When omitted, `--repository-id`
implies `github` and `--repository-uuid` implies `bitbucket`. Otherwise the
account match decides, as Accounts describes. The CLI lists the links of the
allowed sources only.

One invocation serves one source, so `--repository-id` and
`--repository-uuid` do not combine.

## Sync every repository

`--all` syncs every repository that the account's link can read and creates a
project for every repository not yet synced. It needs `--account` and takes no
repository names, `--repository-id` or `--repository-uuid`.

The CLI lists every repository first, then sends sync requests of at most 100
repositories, the server limit. It reports each request on stderr. A
repository that the listing returns twice counts once.

On Bitbucket, `--all` syncs only the repositories whose scan state is
`enabled`, because the server refuses the others. The footer reports how many
it left out. When no repository is enabled, the command fails and points to
`safedep integration bitbucket allowlist update`.

When a request fails after earlier requests wrote projects, the command stops,
exits non-zero, and reports how many repositories it synced. The error shows
the server message. When the failure is temporary (unavailable, timeout or
rate limit), it says to rerun the same command. A rerun is safe because the
sync is idempotent.

## Flags

| Flag | Description |
|------|-------------|
| `--source <github\|bitbucket>` | Repository source. See Sources for the default. |
| `--account <name>` | GitHub organization or user, or Bitbucket workspace slug, to sync through. See Accounts. |
| `--all` | Sync every repository the account can read. See Sync every repository. |
| `--repository-id <id>` | Immutable GitHub repository ID to sync instead of a name. Repeat the flag, or pass a comma-separated list, to select multiple repositories. |
| `--repository-uuid <uuid>` | Immutable Bitbucket repository UUID to sync instead of a name. Braced and uppercase forms are accepted and normalized. Repeat for multiple repositories. Get UUIDs from `safedep integration bitbucket repository list`. |

Inherits root flags `--output` and `--profile`.

## Examples

Sync one repository:

```bash
safedep project sync safedep/cli
```

Sync repositories of two GitHub organizations in one command:

```bash
safedep project sync safedep/cli safedep/vet acme/app
```

Sync by immutable repository ID, skipping name resolution:

```bash
safedep project sync --account safedep --repository-id 1296269 --repository-id 10270250
```

Sync a Bitbucket repository by name, or by UUID:

```bash
safedep project sync safedep/vet-pipe --source bitbucket
safedep project sync --repository-uuid d6c2f02b-7f78-49a8-b7e3-cf8b86efd115
```

Sync every repository of an organization:

```bash
safedep project sync --account safedep --all
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
| `link_id` | Top level: the source link the sync ran through, when one link served the run. Omitted when the run used more than one link. Per project: the link of that project. |
| `account` | Top level: the account of that link, when one link served the run. Per project: the account of that project's link. Omitted when the link has no account name. |
| `repository_id` | GitHub only: the immutable repository ID. |
| `repository_uuid` | Bitbucket only: the immutable repository UUID. |
| `repository_name` | The repository name as the source reports it. Empty for repositories selected by ID or UUID, which the CLI never resolved. |
| `project_id` | The stable SafeDep project ID for that repository. |

An unresolved repository name renders as `-` in table and plain output and is
omitted from JSON. The sync response does not say if a project is new or
already existed, so the output reports only the count.

JSON shape:

```json
{
  "link_id": "link-id",
  "account": "safedep",
  "projects": [
    {
      "repository_id": 1296269,
      "repository_name": "safedep/cli",
      "project_id": "project-id",
      "account": "safedep",
      "link_id": "link-id"
    }
  ]
}
```

`table` renders one row per synced repository with an `ACCOUNT` column, and
names the account in the footer, for example
`3 repositories synced through account safedep`. A link without an account
name shows as `link <id>`. With `--all`, the footer reads
`Synced 240 repositories through account safedep`, and on Bitbucket
`Synced 240 of 300 repositories through account safedep, 60 not enabled`.

`plain` renders a tab-separated header and one tab-separated row per synced
repository. The columns are the repository name, the repository ID or UUID,
the project ID, and the account last, with `-` for a link without an account
name. Every row has the same field count and can be cut by column.

## Resolution and failure behaviour

The CLI validates the batch size, the `OWNER/REPOSITORY` shape, and duplicate
selectors locally, then resolves the source links and every repository name
before it sends a sync request. A name that its link cannot reach fails the
command without materializing any project, and the error names the repository
and the account. Grant it to the GitHub App installation and retry, or retry
with the immutable selector: `--repository-id` for GitHub, `--repository-uuid`
for Bitbucket. Both immutable selectors skip name resolution.

After resolution, each request carries the immutable selector values first,
followed by resolved names in argument order. A name that resolves to an
already selected repository is rejected locally.

Control Tower validates every repository against the link before it writes, so
a failed request materializes no projects. Earlier requests of the same run
keep their projects. Installation, repository access, and suspension failures
preserve their gRPC status details internally. The CLI prints a concise
message, a stable error code, and recovery guidance instead of raw nested gRPC
text.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Every requested repository has a SafeDep project. |
| non-zero | Local validation, link or repository resolution, authentication, or a sync request failed. |
