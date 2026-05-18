# SafeDep CLI

The SafeDep Platform CLI. The unified DevEx layer over [vet](https://github.com/safedep/vet),
[pmg](https://github.com/safedep/pmg), [gryph](https://github.com/safedep/gryph), and [SafeDep
Cloud](https://docs.safedep.io/cloud/overview).

## Installation

```bash
brew install safedep/tap/cli
```

## Commands

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
| [`safedep version`](./docs/cmd/version.md) | Print CLI version |
| [`safedep integration jfrog run`](./docs/cmd/integration-jfrog-run.md) | Push SafeDep malware findings to JFrog XRay |
| [`safedep protect mcp install`](./docs/cmd/protect-mcp-install.md) | Inject SafeDep MCP server config into detected AI agents |
| [`safedep protect mcp uninstall`](./docs/cmd/protect-mcp-uninstall.md) | Remove SafeDep MCP server config from detected AI agents |
