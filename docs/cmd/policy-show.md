# safedep policy show

Show a summary of the policy of the tenant: the head policy revision, the open
policy changes, the package rules, the SafeDep default policy and the policy
overrides in priority order.

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
Package rules   211 (184 trust, 27 block) · 23 expire in 7d
Default policy  revision 1 · scan_gate.fail_on: high · scan_gate.fail_on_attacks: fail

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
