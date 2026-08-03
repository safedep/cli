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
non-empty. Filters combine: a scan must match every supplied filter to appear
in the response.

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

## Output

| Field | Description |
|-------|-------------|
| `scan_session_id` | The scan session's ID. |
| `scan_url` | URL for the scan in the SafeDep web application. Omitted from table output for width. |
| `project_id` | ID of the scanned project. Omitted from JSON when the server does not supply it. |
| `project_name` | Name of the scanned project. |
| `project_version` | Version (branch, tag, or commit) that was scanned. |
| `status` | Lowercase scan status, such as `queued` or `success`. |
| `trigger` | Lowercase scan trigger, such as `push` or `manual`. |
| `created_at` | Scan creation time in UTC RFC 3339 format. Table output humanizes it. |
| `vulnerabilities` | Count of vulnerabilities found by the scan. |
| `policy_violations` | Count of policy violations found by the scan. |
| `suspicious_packages` | Count of suspicious packages found by the scan. |

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
