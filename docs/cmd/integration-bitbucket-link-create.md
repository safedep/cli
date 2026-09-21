# safedep integration bitbucket link create

Issue the link code that connects a Bitbucket workspace to the active SafeDep
tenant.

## Synopsis

```
safedep integration bitbucket link create [--output table|plain|json]
```

## Description

The SafeDep Forge app adds a SafeDep settings page to every Bitbucket
workspace it is installed on. A workspace admin pastes a link code into that
page to link the workspace to a SafeDep tenant. This command issues that
code for the active tenant.

The code is short-lived and single-use. It is shown once: the server stores
only a digest. Issuing a new code invalidates every earlier unredeemed code
of the tenant, so re-running the command is the way to re-generate a code.

## Examples

```bash
safedep integration bitbucket link create
```

```bash
safedep integration bitbucket link create --output json | jq -r '.link_code'
```

## Output

| Field | Description |
|-------|-------------|
| `link_code` | The code to paste into the workspace's SafeDep settings page in Bitbucket. |
| `expires_at` | The instant the code expires, RFC 3339 UTC. |

## Authentication

Requires a control-plane OAuth session with the tenant admin role. Run
`safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | A code was issued. |
| non-zero | Authentication or the request failed. |
