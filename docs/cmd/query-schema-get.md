# safedep query schema get

Fetch the SQL schema served by SafeDep Cloud: tables with typed columns, capability flags, enum values, join edges, and the server's usage rules.

## Synopsis

```
safedep query schema get [--table <name>]... [--output table|plain|json] [--profile <name>]
```

This is the discovery entry point for both humans writing SQL and AI agents composing queries. One call returns everything needed to write a valid query without trial and error.

## Flags

| Flag | Description |
|---|---|
| `--table <name>` | Limit output to the named table. Repeatable (`--table a --table b`) and accepts comma-separated values. Unknown names produce an error listing the available tables. |

Inherits root flags `--output` and `--profile`.

## Output

For each queryable table, prints its columns with their server-declared type, capability flags, and enum values. Join edges and usage rules (with example queries) follow.

| Mode | Format |
|---|---|
| `table` | One section per table: a header line with the table name (and description and time-window metadata when present), then a compact `Column / Type / Caps / Notes` table. Long enum lists are truncated to the first 3 values with a `(+N more)` indicator. Reference URLs surface as a `refs:` footnote under the table when present. A `Joins` table and a `Usage` section (rules + example queries) follow. |
| `plain` | One line per column: `<table>.<col>\t<type>\t<caps>\t<enum-csv>`. Join edges, usage rules, and example queries are appended as `#`-prefixed comment lines so the stream stays grep-friendly. |
| `json` | The full schema in a stable shape. See below. |

Tables and columns are sorted alphabetically so output is stable across runs.

### Caps codes (table mode)

`sel` = selectable, `fil` = filterable, `grp` = groupable, `agg` = aggregatable, `idx` = indexed. `-` when none. The `plain` mode uses the long-form names (`selectable,filterable,...`) for backward-compatible scripting.

### JSON shape

```json
{
  "tables": [
    {
      "name": "projects",
      "description": "...",
      "columns": [
        {
          "name": "origin_source",
          "type": "ENUM",
          "selectable": true,
          "filterable": true,
          "groupable": true,
          "indexed": true,
          "enum_values": [ { "name": "SOURCE_GITHUB", "number": 1 } ]
        },
        {
          "name": "name",
          "type": "STRING",
          "selectable": true,
          "filterable": true,
          "indexed": true
        }
      ]
    }
  ],
  "edges": [
    { "from": "packages", "to": "boms", "cardinality": "many_to_one" }
  ],
  "usage": {
    "rules": [ "Every query must filter on an indexed column or a bounded time range. ..." ],
    "example_queries": [ "SELECT projects.id, projects.name FROM projects WHERE projects.name LIKE 'my-app%'" ]
  }
}
```

`type` is one of `STRING`, `INT`, `FLOAT`, `BOOL`, `TIMESTAMP`, `ENUM`, `UNSPECIFIED`. Empty `enum_values`, `description`, `reference_url`, and `time_column` are omitted; `time_window_max_days` is emitted only when non-zero. `cardinality` is the server string verbatim (`one_to_one`, `one_to_many`, `many_to_one`).

When `--table` filters are applied, `edges` is narrowed to edges where both endpoints are in the filter set; `usage` carries through unchanged.

## Examples

```bash
safedep query schema get -o json                       # full schema for agents
safedep query schema get --table endpoints             # one table, human-readable
safedep query schema get --table packages --table boms # multiple tables and joins between them
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

- `0` on success.
- `1` on any failure: missing credentials, unknown `--table` name, server-side error.
