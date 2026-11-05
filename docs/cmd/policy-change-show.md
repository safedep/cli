# safedep policy change show

Show a policy change. For an open change, the server previews it against head:
the changed fields and their change direction, the changes to policy overrides,
the package rule operations, the subjects that it reaches, how well they enforce
it, and warnings.

## Synopsis

```
safedep policy change show CHANGE [--output table|plain|json]
```

## Examples

```bash
safedep policy change show 18
```

```
Change 18 "payments: medium + cooldown" · open · base rev 46 · head rev 46 · no conflict

Author   alice
Updated  2m ago

What changes
WHERE                WHAT                                OLD -> NEW                                  DIRECTION
override "payments"  scan_gate.fail_on                   high -> medium                              TIGHTENS
override "payments"  package_admission.cooldown.min_age  - -> 7d                                     TIGHTENS
override "payments"  selector                            custom/team=pay -> custom/criticality=high  LOOSENS

Impact · subjects matched: 3 projects, 0 endpoints
FIELD                       ENFORCED  PARTIAL  DETECT ONLY  NOT SUPPORTED
scan_gate.fail_on = medium  2         1        0            0

Warnings
No warnings.

Direction  LOOSENS. Only an Admin can publish it, and Admins and Owners get a notice
```

A stale change shows `STALE: sync it before you publish` in the first line. A
published or deleted change shows no preview.

## Output

`json` holds `change` and `preview` (head revision, stale, direction, changes,
matched counts, coverage, warnings). `plain` writes `key<TAB>value` lines and one
`change<TAB>where<TAB>what<TAB>old<TAB>new<TAB>direction` line for each change.
Values in `plain` and `json` are exact, as `168h`, and a selector is the protojson
that the server sends. The table humanizes them, as `7d` and `custom/criticality=high`.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The change was read. |
| non-zero | Authentication or the request failed. |
