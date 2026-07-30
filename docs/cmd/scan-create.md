# safedep scan create

Submit on-demand scans for one or more SafeDep projects.

## Synopsis

```
safedep scan create [PROJECT_ID...] [--project-name NAME] [--output table|plain|json]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `PROJECT_ID` | SafeDep project ID to scan. IDs are submitted before projects selected by name. |

Supply between 1 and 100 projects in total across positional IDs and
`--project-name` flags. The command sends the resolved batch in one atomic
request and returns after Control Tower admits every scan. It does not wait for
execution or read scan results.

## Flags

| Flag | Description |
|------|-------------|
| `--project-name <name>` | Exact tenant-scoped project name to scan. Repeat the flag to select multiple projects by name. |

Inherits root flags `--output` and `--profile`.

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

Submit one project:

```bash
safedep scan create project-id
```

Submit one project by exact name:

```bash
safedep scan create --project-name safedep/control-tower
```

Mix an ID with names:

```bash
safedep scan create project-id \
  --project-name safedep/control-tower \
  --project-name safedep/cli
```

Submit a batch and receive JSON:

```bash
safedep scan create project-one project-two --output json
```

## Output

Every output mode includes these fields in server response order:

| Field | Description |
|-------|-------------|
| `project_id` | The admitted project's ID. |
| `scan_session_id` | The newly admitted scan session's ID. |
| `status` | Lowercase scan status, such as `queued`. |
| `created_at` | Admission time in UTC RFC 3339 format. Empty in table and plain output, and omitted from JSON, when the server does not supply it. |

JSON shape:

```json
{
  "scans": [
    {
      "project_id": "project-id",
      "scan_session_id": "session-id",
      "status": "queued",
      "created_at": "2026-07-30T10:00:00Z"
    }
  ]
}
```

`table` renders one row per admitted scan. `plain` renders a tab-separated
header and one tab-separated row per admitted scan.

## All-or-nothing admission

The CLI validates batch size and empty or duplicate selectors, then resolves
every exact project name before admission. A missing or ambiguous name fails
the command without submitting any scans. When multiple projects share a name,
the error lists their IDs, sources, and origin URLs so the caller can retry
with a project ID.

After resolution, the CLI rejects duplicate canonical project IDs and sends
one request with positional IDs first, followed by resolved names in flag
order. Control Tower admits the complete batch or rejects it without partially
admitting scans. Authentication, project access, source support, active-scan,
and quota errors are returned without parsing or discarding their gRPC status
details.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Control Tower admitted every requested scan. |
| non-zero | Local validation, project-name resolution, authentication, or batch admission failed. |
