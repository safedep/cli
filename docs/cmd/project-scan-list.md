# safedep project scan list

List scan sessions run by SafeDep Cloud-hosted scanners for the active tenant.

## Synopsis

```
safedep project scan list [--project NAME] [--project-version VERSION]
                          [--status STATUS] [--trigger TRIGGER]
                          [--limit N] [--page-token TOKEN]
                          [--output table|plain|json]
```

The command reads one page per invocation. When the response carries a
continuation token, pass it back with `--page-token` to read the next page.

## Flags

| Flag | Description |
|------|-------------|
| `--project <name>` | Filter to an exact project name. Repeat for up to 10 projects. |
| `--project-version <version>` | Filter to an exact project version. Repeat for up to 10 versions. |
| `--status <status>` | Filter by scan status: `success`, `error`, `queued`, `running`. |
| `--trigger <trigger>` | Filter by scan trigger: `push`, `pull-request`, `tag`, `manual`, `scheduled`. |
| `--limit <n>` | Page size. The server default applies when 0 or omitted. |
| `--page-token <token>` | Continuation token from a prior response. |

Repeated `--project` and `--project-version` values must be unique and
non-empty. Repeated values within one filter match any of them. Different
filters combine, so a scan must satisfy every filter that was supplied. Project
and version names are matched exactly and case-sensitively, as the API compares
them literally, and each value is limited to 255 characters.

Inherits root flags `--output` and `--profile`.

## Examples

List the most recent page of scans:

```bash
safedep project scan list
```

Follow the failing scans of one project:

```bash
safedep project scan list --project safedep/control-tower --status error
```

List scans that a pull request triggered, ten at a time:

```bash
safedep project scan list --trigger pull-request --limit 10
```

Read the next page:

```bash
safedep project scan list --trigger pull-request --limit 10 --page-token <token>
```

Pipe scan session IDs into another command:

```bash
safedep project scan list --status success --output json \
  | jq -r '.scans[].scan_session_id'
```

Re-scan every project that failed its last scan, using plain output:

```bash
safedep project scan list --status error --output plain \
  | tail -n +2 | cut -f2 \
  | xargs -I{} safedep project scan create --project-id {}
```

## Output

| Field | Modes | Description |
|-------|-------|-------------|
| `scan_session_id` | all | The scan session's ID. |
| `project_id` | plain, json | ID of the scanned project. Feed it to `project scan create --project-id`. Omitted from JSON when the server does not supply it. |
| `project_name` | all | Name of the scanned project. |
| `project_version` | all | Version (branch, tag, or commit) that was scanned. |
| `status` | all | Lowercase scan status, such as `queued` or `success`. |
| `trigger` | all | Lowercase scan trigger, such as `push` or `manual`. |
| `vulnerabilities` | all | Count of vulnerabilities found by the scan. |
| `policy_violations` | all | Count of policy violations found by the scan. |
| `suspicious_packages` | all | Count of suspicious packages found by the scan. |
| `created_at` | all | Scan creation time in UTC RFC 3339 format. Table output humanizes it. |
| `scan_url` | plain, json | URL for the scan in the SafeDep web application. |

`project_id` and `scan_url` are absent from table output: eleven columns, two of
them long opaque identifiers, do not fit a terminal. Both are machine-facing, so
they live in plain and JSON where consumers actually read them.

The three counts are computed by the server and can be absent, which is not the
same as zero. An absent count renders as `-` in table and plain output and is
omitted from JSON.

JSON shape:

```json
{
  "scans": [
    {
      "scan_session_id": "session-id",
      "scan_url": "https://app.safedep.io/scans/session-id",
      "project_id": "project-id",
      "project_name": "safedep/control-tower",
      "project_version": "main",
      "status": "success",
      "trigger": "push",
      "created_at": "2026-07-30T10:00:00Z",
      "vulnerabilities": 3,
      "policy_violations": 1,
      "suspicious_packages": 0
    }
  ],
  "next_page_token": "token"
}
```

`table` renders one row per scan and reports the continuation token in the
footer when more pages exist. `plain` renders a tab-separated header and one
tab-separated row per scan, so every row has the same field count and can be cut
by column. The continuation token is not part of plain output: read it from
`table` or `json`.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was retrieved, including when it contains no scans. |
| non-zero | Local validation, authentication, or the list request failed. |
