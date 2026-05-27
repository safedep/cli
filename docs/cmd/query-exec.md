# safedep query exec

Execute a single SQL statement against SafeDep Cloud's query service and print the rows.

## Synopsis

```
safedep query exec --sql "<statement>" [--limit N] [--output table|plain|json]
safedep query exec --sql-file path/to/query.sql [--limit N]
echo "select 1" | safedep query exec
safedep query exec --sql "<statement>" --page-token <token>
```

## Flags

| Flag | Description |
|---|---|
| `--sql, -s <statement>` | SQL statement to execute. Overrides `--sql-file` and stdin. |
| `--sql-file <path>` | Path to a file containing the SQL statement. |
| `--limit <n>` | Maximum rows to return. Range 1-100. Default 100. |
| `--page-token <token>` | `next_page_token` from a prior response, to fetch the following page. Max 2048 chars. |

Inherits root flags `--output` and `--profile`.

## Inputs (precedence)

1. `--sql`
2. `--sql-file`
3. stdin (only when neither flag is set and stdin is not a TTY)

If none yield a non-empty statement, the command fails with a clear message.

## Pagination

The JSON response includes `next_page_token` when more rows are available. To fetch the next page, re-run the same query with `--page-token <value>`. Pagination is caller-driven: the CLI does not auto-iterate. Bounds (`--limit` <= 100, token length <= 2048) match the proto's `buf.validate` constraints so you get a clear client-side error instead of a wrapped gRPC validation message.

```bash
TOK=$(safedep query exec --sql "select * from packages" -o json | jq -r .next_page_token)
safedep query exec --sql "select * from packages" --page-token "$TOK"
```

## Validation

- The statement is trimmed and any trailing `;` is removed before sending.
- Empty statements are rejected client-side.
- Statements larger than 16000 bytes are rejected client-side (matches the server-side `buf.validate` bound on `QueryBySqlRequest.query`).
- Multi-statement queries are rejected by the server. The CLI does not parse SQL.
- Server validation errors (unknown table, unknown column, unsupported construct) are passed through verbatim so agents can self-correct.

## Output modes

| Mode | Format |
|---|---|
| `table` | Rendered table with one row per record, using the server's column order. A summary footer follows: `<count> rows \| ~<estimated_cost> cost \| <elapsed_ms>ms`. When more rows are available, a second footer line prints `next page: --page-token <token>`. |
| `plain` | Tab-separated header followed by one tab-separated row per record. No footer (pipeline-safe). |
| `json` | Typed columns, planner stats, and the next-page cursor. See shape below. |

JSON shape:

```json
{
  "columns": [
    { "name": "projects.name", "type": "STRING" },
    { "name": "n", "type": "INT" }
  ],
  "rows": [ { "projects.name": "acme/web", "n": 3 } ],
  "count": 1,
  "next_page_token": "",
  "generated_at": "2026-05-24T04:43:34Z",
  "stats": { "estimated_cost": 1243.7, "estimated_rows": 1, "elapsed_ms": 9 }
}
```

`type` is one of `STRING`, `INT`, `FLOAT`, `BOOL`, `TIMESTAMP`, `ENUM`, `UNSPECIFIED`. Row values are decoded as JSON-native types (string, number, bool, null); the column `type` is advisory metadata for the consumer, not a re-cast of the row value. `next_page_token` is omitted when empty; `generated_at` is omitted when zero.

When the result set is empty, `table` and `plain` print `no rows`. JSON returns `"count": 0` with an empty `rows` array, and `columns` is still populated so consumers see the shape.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

- `0` on success.
- `1` on any failure: missing credentials, invalid input, server-side query error.
