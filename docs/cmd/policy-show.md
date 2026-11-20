# safedep policy show

Show a summary of the policy of the tenant: the head policy revision, the open
policy changes, the package rules, the policy settings and the policy overrides
in priority order.

The settings section lists one setting on each row, grouped by the parent of
its path, as `scan_gate.licenses`. Each row shows the value that every subject
gets and the layer that sets it: `base`, `default policy` (the SafeDep default
policy), or `-` when no layer sets it. Below the row, each policy override that
sets the field shows its value, from the highest priority to the lowest. An
override value applies only to the subjects that its selector matches. A long
value, such as a license list, wraps to the terminal width.

## Synopsis

```
safedep policy show [--output table|plain|json]
```

## Flags

The command has no flags of its own. It inherits the root flags `--output` and
`--profile`.

## Examples

```bash
safedep policy show
```

```
Policy          platform-eng.acme.safedep.io
Head revision   46 · published 2d ago by alice · "cooldown 5d for laptops"
Open changes    none
Package rules   211 (184 trust, 27 block) · 23 expire in 7d ·
                safedep policy rule list
Default policy  revision 2

Settings
SETTING            SOURCE                     VALUE
scan_gate
  fail_on          base                       medium
                   override "ci" (300)        critical
                   override "payments" (200)  high
  fail_on_attacks  default policy             fail
scan_gate.licenses
  deny             default policy             GPL-2.0, GPL-2.0-only,
                                              GPL-2.0-or-later, GPL-3.0,
                                              GPL-3.0-only, GPL-3.0-or-later,
                                              AGPL-3.0, AGPL-3.0-only,
                                              AGPL-3.0-or-later
  unknown          default policy             allow
An override sets its value only for the subjects that its selector matches.
See the settings document: safedep policy get

Overrides (priority, highest first)
PRIORITY  NAME      SELECTOR                 EXPIRES  DESCRIPTION
300       ci        custom/environment=ci    -        -
200       payments  custom/criticality=high  -        -
```

Read the policy settings document as JSON:

```bash
safedep policy show -o json | jq '.policy'
```

## Output

`json` holds `head_revision`, `head` (the revision record), `default_policy_revision`,
`default_policy` (path to value), `open_change_count`, `package_rule_counts`,
`overrides` (id, name, priority, selector, description, expiry) and `policy`, the
policy settings document in the same form as `safedep policy get`.

`plain` writes one `key<TAB>value` line for each count, and one
`override<TAB>priority<TAB>name<TAB>selector<TAB>expires_at` line for each policy
override.

The fleet rows of the design (delivery to endpoints, project counts, notices)
need the fleet view of a later milestone. The command does not show them yet.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The policy was read. |
| non-zero | Authentication or the request failed. |
