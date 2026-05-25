[![npm](https://img.shields.io/npm/v/@safedep/cli?style=flat-square)](https://www.npmjs.com/package/@safedep/cli)
[![License](https://img.shields.io/github/license/safedep/cli?style=flat-square)](LICENSE)
[![CI](https://img.shields.io/github/actions/workflow/status/safedep/cli/goreleaser.yml?style=flat-square)](https://github.com/safedep/cli/actions)

# SafeDep CLI

`safedep` is SafeDep Cloud on the command line. Manage auth, query endpoint
telemetry, harden AI coding agents, and push findings to your security stack —
for humans and the agents they work with.

## Protect your AI agents in one command

```bash
npx @safedep/cli setup mcp install
```

One command authenticates you with SafeDep Cloud, detects your AI coding agents
(Claude Code, Cursor, Gemini CLI, and more), and injects MCP-based threat protection
into each one.

## Install

```bash
brew install safedep/tap/cli
```

```bash
npm install -g @safedep/cli
```

```bash
pnpm add -g @safedep/cli
```

```bash
bun add -g @safedep/cli
```

<details>
<summary>Other installation options</summary>

Download prebuilt binaries for Linux, macOS, and Windows from the
[GitHub Releases](https://github.com/safedep/cli/releases) page.

</details>

## Get started

```bash
# Authenticate with SafeDep Cloud
safedep auth login

# Check your endpoint fleet
safedep endpoint status

# Query your security telemetry
safedep query exec --sql "select name, version from packages limit 10"

# Protect your AI coding agents
safedep setup mcp install
```

## What safedep can do

**Authentication and profiles** — `safedep auth`
Log in to SafeDep Cloud, manage credential profiles, and switch between tenants.

**Endpoint fleet intelligence** — `safedep endpoint`
Monitor the health of every endpoint reporting to your tenant, list active machines,
and drill into package inventory and recent activity.

**Security telemetry queries** — `safedep query`
Run SQL against SafeDep Cloud's query service. Inspect packages, events, and findings
across your entire fleet.

**AI agent protection** — `safedep protect mcp`
Inject or remove the SafeDep MCP server from detected AI coding agents. Supports
Claude Code, Cursor, Gemini CLI, and more.

**Integrations** — `safedep integration`
Push SafeDep malware findings to external security tools. JFrog XRay is supported.

## Learn more

- [Documentation](https://docs.safedep.io) — guides, concepts, and API reference
- [SafeDep Cloud](https://app.safedep.io) — the platform behind the CLI
- [GitHub Issues](https://github.com/safedep/cli/issues) — bug reports and feature requests

<details>
<summary>Full command reference</summary>

| Command | Description |
|---------|-------------|
| [`safedep auth login`](./docs/cmd/auth-login.md) | Authenticate with SafeDep Cloud |
| [`safedep auth logout`](./docs/cmd/auth-logout.md) | Remove credentials for the active profile |
| [`safedep auth status`](./docs/cmd/auth-status.md) | Show authentication status |
| [`safedep auth profile list`](./docs/cmd/auth-profile-list.md) | List credential profiles |
| [`safedep endpoint status`](./docs/cmd/endpoint-status.md) | Show fleet health |
| [`safedep endpoint list`](./docs/cmd/endpoint-list.md) | List endpoints with filters |
| [`safedep endpoint show`](./docs/cmd/endpoint-show.md) | Show endpoint detail |
| [`safedep endpoint activity list`](./docs/cmd/endpoint-activity-list.md) | List recent endpoint activity |
| [`safedep endpoint inventory list`](./docs/cmd/endpoint-inventory-list.md) | List current endpoint inventory |
| [`safedep query exec`](./docs/cmd/query-exec.md) | Execute a SQL query against SafeDep Cloud |
| [`safedep query schema get`](./docs/cmd/query-schema-get.md) | Inspect the SafeDep Cloud query schema |
| [`safedep protect mcp install`](./docs/cmd/protect-mcp-install.md) | Inject SafeDep MCP server config into detected AI agents |
| [`safedep protect mcp uninstall`](./docs/cmd/protect-mcp-uninstall.md) | Remove SafeDep MCP server config from detected AI agents |
| [`safedep integration jfrog run`](./docs/cmd/integration-jfrog-run.md) | Push SafeDep malware findings to JFrog XRay |
| [`safedep setup mcp install`](./docs/cmd/setup-mcp-install.md) | Guided onboarding: authenticate and configure AI agents |
| [`safedep version`](./docs/cmd/version.md) | Print CLI version |

</details>
