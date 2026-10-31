# safedep policy change sync

Move the base of an open policy change to the head revision. The change keeps its
settings as you wrote them. The server never merges, so review the change again
before you publish it.

## Synopsis

```
safedep policy change sync CHANGE
```

## Examples

```bash
safedep policy change sync 19
```

```
✓ change 19 rebased on revision 51 · review it: safedep policy change show 19
```

Sync a change when its publish stops because the policy moved.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The change was synced. |
| non-zero | The change is not open, or the request failed. |
