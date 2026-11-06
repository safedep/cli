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
override "payments"  package_admission.cooldown.min_age  - -> 7d                                     LOOSENS
override "payments"  selector                            custom/team=pay -> custom/criticality=high  LOOSENS

Impact · subjects matched: 3 projects, 0 endpoints
FIELD                                      ENFORCED  PARTIAL  DETECT ONLY  NOT SUPPORTED
package_admission.cooldown.min_age = 168h  0         0        0            3
scan_gate.fail_on = medium                 3         0        0            0

Warnings
No warnings.

Direction  LOOSENS. Only an Admin can publish it, and Admins and Owners get a notice
```

The policy base and the SafeDep default policy set no cooldown, so the server
counts the first cooldown of the override as a change that loosens: it does not
know what each tool did before. A selector edit always loosens, because it can
move subjects to another policy override. The impact counts only projects: the
pull request check enforces `scan_gate.fail_on` and does not support the cooldown.

When the change sets a field that its layer did not set before, or unsets a
field, the empty side shows the value that the lower layers give the field, as
`high (default) -> medium` for the policy base and `medium -> high (inherited)`
for a policy override. A side shows `-` when no lower layer sets the field.

An update or a delete of a package rule names the rule at head by its effect,
selector and id, as `delete trust pkg:npm/lodash (r_68e23zskkh)`.

A stale change shows `STALE: sync it before you publish` in the first line. A
published or deleted change shows no preview.

## Output

`json` holds `change` and `preview` (head revision, stale, direction, changes,
matched counts, coverage, warnings). `plain` writes `key<TAB>value` lines and one
`change<TAB>where<TAB>what<TAB>old<TAB>new<TAB>direction<TAB>inherited` line for
each change. `inherited` is the value of the lower layers on an empty side.
Values in `plain` and `json` are exact, as `168h`, and a selector is the protojson
that the server sends. The table humanizes them, as `7d` and `custom/criticality=high`.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The change was read. |
| non-zero | Authentication or the request failed. |
