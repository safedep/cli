# SafeDep CLI

The SafeDep Platform CLI. The unified DevEx layer over [vet](https://github.com/safedep/vet),
[pmg](https://github.com/safedep/pmg), [gryph](https://github.com/safedep/gryph), and [SafeDep
Cloud](https://docs.safedep.io/cloud/overview).

## Commands

| Command | Description |
|---------|-------------|
| [`safedep auth login`](./docs/cmd/auth-login.md) | Authenticate with SafeDep Cloud |
| [`safedep auth logout`](./docs/cmd/auth-logout.md) | Remove credentials for the active profile |
| [`safedep auth status`](./docs/cmd/auth-status.md) | Show authentication status |
| [`safedep auth profile list`](./docs/cmd/auth-profile-list.md) | List credential profiles |
| [`safedep query exec`](./docs/cmd/query-exec.md) | Execute a SQL query against SafeDep Cloud |
| [`safedep query schema get`](./docs/cmd/query-schema-get.md) | Inspect the SafeDep Cloud query schema |
| [`safedep version`](./docs/cmd/version.md) | Print CLI version |
