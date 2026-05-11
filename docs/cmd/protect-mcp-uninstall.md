# safedep protect mcp uninstall

Remove the SafeDep MCP server entry from the configuration files of all AI coding agents detected on this machine.

## Synopsis

```
safedep protect mcp uninstall [flags]
```

## Flags

| Flag | Description |
|---|---|
| `--workspace <dir>` | Project directory for workspace-level removal. Empty (default) skips workspace. |

Inherits root flags `--output`, `--profile`, and `--insecure-keychain-fallback`.

## Authentication

Does not require authentication. The removal is a local filesystem operation.

## What it does

1. Detects which supported AI agents are installed on this machine.
2. Removes the `mcpServers.safedep` entry from each detected agent's config file. All other keys are preserved. No-op if the entry is already absent.

## Exit codes

- `0` on success, including the case where no agents are detected or the entry is already absent.
- `1` on filesystem errors.
