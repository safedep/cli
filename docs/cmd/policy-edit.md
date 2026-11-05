# safedep policy edit

Open the policy settings document of the tenant in your editor. Saving creates a
policy change on the server. Nothing applies until you publish the change.

## Synopsis

```
safedep policy edit [-m TITLE] [--override NAME]
```

## Flags

| Flag | Description |
|------|-------------|
| `-m`, `--message <title>` | The title of the policy change. When empty, the publish message becomes the title. |
| `--override <name>` | Edit only the policy override with this name. |

The editor is `$VISUAL`, then `$EDITOR`, then `vi` (`notepad` on Windows). The
command needs a terminal.

## Examples

```bash
safedep policy edit -m "payments: medium + cooldown"
```

The document has every policy settings field of the contract. A field that the
policy sets is a YAML line. A field that it does not set is a commented line at
its nesting, with a note: the values that the field accepts, and the value of
the layers below and the layer that sets it. When no layer sets a field, each
tool uses its local value. To set a field, remove the `# `
at the start of its line and of the lines of its parents. A save of the document
as it is changes nothing. When the policy has no override, the document ends
with a commented example of one.

The document of a tenant that has not published a policy yet:

```yaml
# Policy of acme.safedep.io · head revision 0
# Saving creates a draft change. Nothing applies until you publish it.
#
# Layers, from low to high, for each subject:
#   1. SafeDep default policy (read-only, revision 1):
#        scan_gate.fail_on: high · scan_gate.fail_on_attacks: fail
#   2. base      - applies to every endpoint and project of this tenant
#   3. overrides - apply to the subjects that their selector matches.
#                  For each field, the highest priority that sets it wins.
#
# Keep the id of each override. A new override has no id.
# Package rules are not in this file: 0 rules (safedep policy rule list)
# The policy keeps no comments. Write the reason for a value in a description field.
# To set a field, remove the "# " at the start of its line and of the lines of its parents.
# Fields and values: https://docs.safedep.io/reference/cloud-policy-settings

# base:
  # package_admission:
    # cooldown:
      # a duration, as 7d or 12h, from 0s to 365d. Now: not set, each tool uses its local value
      # min_age: 7d
  # scan_gate:
    # fail_on: high  # critical, high, medium, low, info. Now: high (default policy, revision 1)
    # fail_on_attacks: fail  # report, fail. Now: fail (default policy, revision 1)
# base_description: Why the base has these values  # text

# An example policy override. It applies to the subjects that its selector matches.
# Remove the "# " at the start of each line to use it, then change the values.
# overrides:
#   - name: payments
#     description: A longer cooldown for the payments team
#     priority: 100
#     selector:
#       matchLabels: {custom/team: payments}
#     spec:
#       package_admission:
#         cooldown:
#           min_age: 7d
```

To set a cooldown of 7 days for the whole tenant, remove the `# ` from the
`base:`, `package_admission:`, `cooldown:` and `min_age:` lines, then save.

The start of the document of a tenant with values, after the header:

```yaml
base:
  # package_admission:
    # cooldown:
      # a duration, as 7d or 12h, from 0s to 365d. Now: not set, each tool uses its local value
      # min_age: 7d
  scan_gate:
    fail_on: high
    # fail_on_attacks: fail  # report, fail. Now: fail (default policy, revision 1)
# base_description: Why the base has these values  # text
overrides:
  - id: ov_pay
    name: payments
    priority: 200
    selector:
      matchLabels: {custom/criticality: high}
    spec:
      # package_admission:
        # cooldown:
          # min_age: 7d  # a duration, as 7d or 12h, from 0s to 365d. Below this override: not set
      scan_gate:
        fail_on: high
        # report, fail. Below this override: fail (default policy, revision 1)
        # fail_on_attacks: fail
```

A note of a base field says the value that the subjects get now. A note of an
override field says the value of the base or the default policy below it. An
override of a lower priority can also set the field for some subjects.

```
✓ change 18 saved from revision 46
  Review it: safedep policy change show 18
```

Edit one policy override:

```bash
safedep policy edit --override payments
```

## Behavior

- Each policy override has its `id`. Keep it when you rename an override. Remove
  an override, or add one with no `id`. The command refuses an `id` that is not in
  the policy, and warns before the save for each override that the edit removes.
  The server refuses to remove an override while package rules attach to it.
- When the policy holds fields that this CLI does not know, the command stops
  before the editor opens, because a save would drop them. Update the CLI.
- An editor that only rewrites line endings or spacing makes no change.
- When you close the editor with no change, the command saves nothing.
- When the file does not parse, or the server rejects it as invalid, the editor
  opens again with the error at the top. Close it with no change to stop.
- The command warns when the file holds a comment that it did not write. The
  policy keeps no comments.
- A parent key that you uncomment with no field below it makes no change.
- The command warns when an added or changed policy override selects on a
  `managed/`, `reported/` or `scm-property/` label. A tool sets such a label, so
  a person on the endpoint can change it. The save continues.
- When the policy has settings and the saved file sets no field, the editor
  opens again with the error. To remove every setting, write `{}` on one line.
- When the save fails for another reason, the command keeps the file and prints
  its path.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The change was saved, or nothing changed. |
| non-zero | The edit or the request failed. |
