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
Rule          r_9c1
Selector      pkg:npm/evil vers:npm/>=1.2.0
Effect        block
Scope         -
Reason        IR-2291 credential stealer
Ticket        IR-2291
Requested by  -
Attaches to   base
Expires       never
Created by    carol
Revisions     added in revision 48 · active
```

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The rule was read. |
| non-zero | The rule does not exist, or the request failed. |
