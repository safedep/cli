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

The file starts with a header that shows the layers:

```yaml
# Policy of platform-eng.acme.safedep.io · head revision 46
# Saving creates a draft change. Nothing applies until you publish it.
#
# Layers, from low to high, for each subject:
#   1. SafeDep default policy (read-only, revision 12):
#        scan_gate.fail_on: high
#   2. base      - applies to every endpoint and project of this tenant
#   3. overrides - apply to the subjects that their selector matches.
#                  For each field, the highest priority that sets it wins.
#
# Keep the id of each override. A new override has no id.
# Package rules are not in this file: 211 rules (safedep policy rule list)
# The policy keeps no comments. Write the reason for a value in a description field.
```

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
- The command warns when the file holds a comment other than its header. The
  policy keeps no comments.
- When the save fails for another reason, the command keeps the file and prints
  its path.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The change was saved, or nothing changed. |
| non-zero | The edit or the request failed. |
