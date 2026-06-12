# safedep protect mcp status

Report, for every supported AI coding agent, whether it is detected on this machine and whether the SafeDep MCP server is configured in its config file.

Agents supported: Claude Code, Cursor, Gemini CLI, OpenCode, Antigravity, VS Code.

## Synopsis

```
safedep protect mcp status [flags]
```

## Flags

| Flag | Description |
|---|---|
| `--workspace <dir>` | Project directory for workspace-level status. Empty (default) skips workspace inspection. |

Inherits root flags `--output`, `--profile`, and `--insecure-keychain-fallback`.

## Authentication

Does not require authentication. The status check is a local filesystem read.

## What it does

1. Detects which supported AI agents are installed on this machine.
2. For each detected agent, reads its config file and reports whether the `safedep` MCP server entry is present.
3. With `--workspace`, also inspects the agent's workspace-level config for that project.

Undetected agents and config scopes an agent does not support are reported as not applicable.

## Exit codes

- `0` on success, including the case where no agents are detected or none are configured.
- `1` on filesystem or config parse errors.
