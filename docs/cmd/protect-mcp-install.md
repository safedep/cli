# safedep protect mcp install

Detect AI coding agents installed on this machine and inject the SafeDep MCP server entry into each agent's config file.

Agents supported: Claude Code, Cursor, Gemini CLI, OpenCode, Antigravity, VS Code.

## Synopsis

```
safedep protect mcp install [flags]
```

## Flags

| Flag | Description |
|---|---|
| `--mcp-url <url>` | SafeDep MCP server URL (default: `https://mcp.safedep.io/model-context-protocol/threats/v1/mcp`). |
| `--workspace <dir>` | Project directory for workspace-level injection. Empty (default) skips workspace injection. |

Inherits root flags `--output`, `--profile`, and `--insecure-keychain-fallback`.

## Authentication

Requires an authenticated session with API key and tenant credentials stored in the keychain. Run `safedep auth login` first, or use `pnpx @safedep/cli setup mcp` for a guided first-time setup.

## What it does

1. Resolves the active profile's API key and tenant from the keychain.
2. Derives the machine's stable endpoint identity (`X-Endpoint-ID`) from the hardware UUID and hostname.
3. Detects which supported AI agents are installed on this machine.
4. Writes the `mcpServers.safedep` entry into each detected agent's config file. The write is idempotent — repeated calls update only the `safedep` key; all other keys in the config file are preserved.

## Exit codes

- `0` on success, including the case where no agents are detected.
- `1` on any error (missing credentials, filesystem failure, endpoint identity failure).
