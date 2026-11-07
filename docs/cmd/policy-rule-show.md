# safedep policy rule show

Show one package rule. A block message of a tool cites the rule id.

## Synopsis

```
safedep policy rule show RULE [--output table|plain|json]
```

## Examples

```bash
safedep policy rule show r_9c1
```

```
Rule             r_9c1
Selector         pkg:npm/evil vers:npm/>=1.2.0
Effect           block
Scope            -
Vulnerabilities  -
Reason           IR-2291 credential stealer
Ticket           IR-2291
Requested by     -
Attaches to      base
Expires          never
Created by       carol
Revisions        added in revision 48 · active
```

For a trust entry that waives named vulnerabilities, the `Vulnerabilities` row
shows each id with its justification:

```
Scope            named-vulnerability
Vulnerabilities  CVE-2024-1234 (vulnerable_code_not_in_execute_path), GHSA-jfh8-c2jp-5v3q
```

`json` holds `rule`, with `vulnerabilities`: an empty list, or each waived `id`
and its `justification`. `plain` writes one tab-separated key and value on each
line. Its `vulnerabilities` value has the form of the `--vulnerability` flag.

After an update of the rule, `Revisions` also names the revision of the last
update, as `added in revision 48, updated in revision 52 · active`.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The rule was read. |
| non-zero | The rule does not exist, or the request failed. |
