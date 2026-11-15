# safedep policy change edit

Open the policy settings of an open policy change in your editor. Saving updates
the change on the server.

## Synopsis

```
safedep policy change edit CHANGE [-m TITLE] [--override NAME]
```

## Flags

| Flag | Description |
|------|-------------|
| `-m`, `--message <title>` | A new title for the change. |
| `--override <name>` | Edit only the policy override with this name. |

A change that does not edit the settings yet starts from the head settings. For a
change that restores a revision, saving replaces the settings that the restore
made, and the command warns first. The document has the same form, with every
settings field and a note for each field that the change does not set, and the
editor behaves as in [`safedep policy edit`](./policy-edit.md).

## Examples

```bash
safedep policy change edit 18
```

```
✓ change 18 updated
  Review it: safedep policy change show 18
```

To change only the title, pass `-m` and close the editor with no change:

```bash
safedep policy change edit 18 -m "payments: medium only"
```

```
✓ change 18: the title is now "payments: medium only". The settings did not change
```

When another admin updated the change after the command read it, the update
stops. Run `safedep policy change show 18` and edit again.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The change was updated, or nothing changed. |
| non-zero | The edit or the request failed. |
