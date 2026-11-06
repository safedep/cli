# safedep project scan create

Submit scans for one or more SafeDep projects to SafeDep Cloud-hosted scanners.

## Synopsis

```
safedep project scan create [PROJECT_NAME...] [--project-id ID] [--branch NAME | --tag NAME | --ref REF] [--output table|plain|json]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `PROJECT_NAME` | Exact tenant-scoped project name to scan. |

Supply between 1 and 100 projects in total across positional names and
`--project-id` flags. The command sends the resolved batch in one atomic
request and returns after Control Tower admits every scan. It does not wait for
execution or read scan results.

Scan execution happens remotely on SafeDep Cloud-hosted scanners. This command
does not scan the project locally or invoke `vet`.

## Flags

| Flag | Description |
|------|-------------|
| `--project-id <id>` | Project ID to scan instead of a name. Repeat the flag to select multiple projects by ID. |
| `--branch <name>` | Branch to scan in every selected project. |
| `--tag <name>` | Tag to scan in every selected project. |
| `--ref <ref>` | Full ref to scan in every selected project, such as `refs/tags/v1.2.0`. A plain name is a branch. |

Inherits root flags `--output` and `--profile`.

## Choose a branch or tag

Pass at most one of these flags. Without any of them, each project scans the
default branch that its repository reports at admission.

| Flag | Example | Scans |
|------|---------|-------|
| `--branch <name>` | `--branch release/1.2` | The branch `release/1.2`. |
| `--tag <name>` | `--tag v1.2.0` | The tag `v1.2.0`. |
| `--ref <ref>` | `--ref refs/tags/v1.2.0` | Exactly that ref, which must start with `refs/heads/` or `refs/tags/`. Use it when you already have a full ref. A plain name is a branch: `--ref main` scans `refs/heads/main`. |

`--branch` and `--tag` take a short name. Pass a full ref with `--ref`.

Control Tower resolves the ref to a commit at admission, and the scan reads that
commit even if the branch moves later. Results are stored under the short name,
for example `release/1.2`. A name that is both a branch and a tag is rejected,
because the two would share that short name.

The ref applies to every selected project. If one repository does not have the
ref, Control Tower rejects the whole batch and the error names the project.

Only GitHub projects take a ref today. A batch with a ref that selects a
Bitbucket project fails as not scannable.

## Discover project IDs

List GitHub-backed projects with the query service:

```bash
safedep query exec --sql \
  "SELECT projects.id, projects.name
   FROM projects
   WHERE projects.origin_source = 'SOURCE_GITHUB'
   ORDER BY projects.name"
```

## Examples

Submit one project by exact name:

```bash
safedep project scan create safedep/control-tower
```

Submit one project by ID:

```bash
safedep project scan create --project-id 01K88WX3G9RGAK8T3N5YJMFQN1
```

Mix names with an ID:

```bash
safedep project scan create safedep/control-tower safedep/cli \
  --project-id 01K88WX3G9RGAK8T3N5YJMFQN1
```

Scan a release branch in two projects:

```bash
safedep project scan create safedep/control-tower safedep/cli --branch release/1.2
```

Scan a tag:

```bash
safedep project scan create safedep/cli --tag v1.2.0
```

Scan the branch or tag that a GitHub Actions push or tag workflow runs on:

```bash
safedep project scan create safedep/cli --ref "$GITHUB_REF"
```

In a GitHub Actions pull request workflow, `GITHUB_REF` is
`refs/pull/<n>/merge`, a merge commit that GitHub builds for the pull request.
It is not a branch or a tag, so the CLI rejects it. Scan the pull request's
source branch instead:

```bash
safedep project scan create safedep/cli --branch "$GITHUB_HEAD_REF"
```

This works when the source branch is in the scanned repository. A branch in a
fork is not, and the scan fails as not found.

Submit a batch and receive JSON:

```bash
safedep project scan create safedep/control-tower safedep/cli --output json
```

## Output

Every output mode includes these fields in server response order:

| Field | Description |
|-------|-------------|
| `project_id` | The admitted project's ID. |
| `scan_session_id` | The newly admitted scan session's ID. |
| `scan_url` | URL for the admitted scan in the SafeDep web application. |
| `status` | Lowercase scan status, such as `queued`. |
| `created_at` | Admission time in UTC RFC 3339 format. Empty in table and plain output, and omitted from JSON, when the server does not supply it. |

JSON shape:

```json
{
  "scans": [
    {
      "project_id": "project-id",
      "scan_session_id": "session-id",
      "scan_url": "https://app.safedep.io/scans/session-id",
      "status": "queued",
      "created_at": "2026-07-30T10:00:00Z"
    }
  ]
}
```

`table` renders one row per admitted scan. `plain` renders a tab-separated
header and one tab-separated row per admitted scan.

## All-or-nothing admission

The CLI validates batch size, the ref flags, and empty or duplicate
selectors, then resolves every exact project name before admission. A missing or ambiguous name fails
the command without submitting any scans. When multiple projects share a name,
the error lists their IDs, sources, and origin URLs so the caller can retry
with a project ID.

After resolution, the CLI rejects duplicate canonical project IDs and sends
one request with flag-selected IDs first, followed by resolved names in
argument order. Control Tower admits the complete batch or rejects it without partially
admitting scans. Authentication, project access, source support, active-scan,
and quota errors preserve their gRPC status details internally. The CLI uses
typed SafeDep error reasons when available and prints a concise message, stable
error code, and recovery guidance instead of raw nested gRPC text.
Expected local selector and project-name resolution failures use the same
presentation, including a recovery action. A ref that a repository does not
have, or a name that is both a branch and a tag, fails with the server's message
naming the project and the ref.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Control Tower admitted every requested scan. |
| non-zero | Local validation, project-name resolution, authentication, or batch admission failed. |
