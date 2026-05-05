# safedep query exec

Execute a single SQL statement against SafeDep Cloud's query service and print the rows.

## Synopsis

```
safedep query exec --sql "<statement>" [--limit N] [--output table|plain|json]
safedep query exec --sql-file path/to/query.sql [--limit N]
echo "select 1" | safedep query exec
```

## Flags

| Flag | Description |
|---|---|
| `--sql, -s <statement>` | SQL statement to execute. Overrides `--sql-file` and stdin. |
| `--sql-file <path>` | Path to a file containing the SQL statement. |
| `--limit <n>` | Maximum rows to return. Range 1-10000. Default 100. |

Inherits root flags `--output` and `--profile`.

## Inputs (precedence)

1. `--sql`
2. `--sql-file`
3. stdin (only when neither flag is set and stdin is not a TTY)

If none yield a non-empty statement, the command fails with a clear message.

## Validation

- The statement is trimmed and any trailing `;` is removed before sending.
- Empty statements are rejected client-side.
- Statements larger than 64KiB are rejected client-side.
- Multi-statement queries are rejected by the server. The CLI does not parse SQL.

## Output modes

| Mode | Format |
|---|---|
| `table` | Rendered table with one row per record. |
| `plain` | Tab-separated header followed by one tab-separated row per record. |
| `json` | `{ "columns": [...], "rows": [{col: val}, ...], "count": N, "next_page_token": "..." }`. |

When the result set is empty, `table` and `plain` print `no rows`. JSON returns `"count": 0` with an empty `rows` array.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

- `0` on success.
- `1` on any failure: missing credentials, invalid input, server-side query error.
