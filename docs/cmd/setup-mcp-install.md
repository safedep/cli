# safedep setup mcp install

Guided first-time onboarding: authenticate with SafeDep Cloud and inject the SafeDep MCP server into AI coding agent config files in a single command.

Agents supported: Claude Code, Cursor, Gemini CLI, OpenCode, Antigravity.

## Synopsis

```
safedep setup mcp install [flags]
```

## Flags

| Flag | Description |
|---|---|
| `--mcp-url <url>` | SafeDep MCP server URL (default: `https://mcp.safedep.io/model-context-protocol/threats/v1`). |
| `--workspace <dir>` | Project directory for workspace-level injection. Empty (default) skips workspace injection. |
| `--force` | Bypass credential check and always re-authenticate via device flow. |

Inherits root flags `--output`, `--profile`, and `--insecure-keychain-fallback`.

## What it does

1. Checks for existing credentials (API key + tenant) in the active profile's keychain. If found and `--force` is not set, skips authentication and goes directly to agent configuration.
2. If no credentials exist (or `--force` is set): runs the OAuth2 device-code flow, prompts for tenant selection (and registration if the account is new), creates an API key, and saves everything to the keychain.
3. Derives the machine's stable endpoint identity (`X-Endpoint-ID`) from the hardware UUID and hostname.
4. Detects which supported AI agents are installed on this machine.
5. Writes the `mcpServers.safedep` entry into each detected agent's config file. The write is idempotent.

If authentication succeeds but agent configuration fails, credentials are kept and an advisory message is printed. Use `safedep protect mcp install` to retry the configuration step.

## Exit codes

- `0` on success, including the case where no agents are detected.
- `1` on authentication or keychain error.
