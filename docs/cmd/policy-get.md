# safedep policy get

Write the policy settings document of the tenant as YAML: the policy base and
every policy override. Package rules are not in the document.

## Synopsis

```
safedep policy get [--revision N] [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--revision <n>` | The policy revision to write. The head revision when 0 or omitted. |

Inherits root flags `--output` and `--profile`.

## Examples

Write the head settings to a file:

```bash
safedep policy get > policy.yaml
```

```yaml
# Policy settings of platform-eng.acme.safedep.io · revision 46
# Package rules are not in this file: safedep policy rule list

base:
  scan_gate:
    fail_on: high
overrides:
  - id: ov_7c2
    name: payments
    description: Stricter gate for payment services (CUST-412)
    priority: 200
    selector:
      matchLabels: {custom/criticality: high}
    spec:
      scan_gate:
        fail_on: medium
```

Read an old revision:

```bash
safedep policy get --revision 40
```

## Output

`table` and `plain` write the YAML document. `json` writes
`{"revision": N, "policy": {...}}` with the same document.

The document form:

- Field names are the contract names, as in `scan_gate.fail_on`.
- An enum value is a short token, as in `medium` for `SEVERITY_MEDIUM`.
- A duration is a Go duration, as in `168h`. An edit also accepts days, as in `7d`.
- A selector has the Kubernetes form: `matchLabels` and `matchExpressions`.
- Each policy override has its `id`, which the server sets and keeps. A rename
  keeps the id. A policy override with no id is a new one.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The document was written. |
| non-zero | Authentication or the request failed. |
