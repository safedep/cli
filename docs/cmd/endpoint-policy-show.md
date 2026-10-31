# safedep endpoint policy show

Show the intended policy of one endpoint: each field, its value, and its source.
The output also shows the applied policy revision and the exceptions that each
tool reported, such as a field that a managed file locks.

## Synopsis

```
safedep endpoint policy show ENDPOINT [--field PATH]... [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--field <path>` | Show only this contract path, or the paths below it. Repeatable. |

## Examples

```bash
safedep endpoint policy show arjuns-mbp --field package_admission.cooldown.min_age
```

```
Endpoint        arjuns-mbp (01KR0EKN6PMW0ZRFRN992H1PKX)
Policy          revision 52 · overrides laptops (100)
Default policy  revision 12
Package rules   211 (184 trust, 27 block)

FIELD                               VALUE  SOURCE                    EXCEPTIONS
package_admission.cooldown.min_age  5d     override "laptops" (100)  pmg: locked (floor in /etc/safedep/cloud.yml)

Applied policy, as each tool reported it
TOOL  VERSION  REVISION  STATE    FETCHED  REPORTED
pmg   2.3.1    52        applied  3m ago   3m ago
```

The output format is the same as for
[`safedep project policy show`](./project-policy-show.md).

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The policy was read. |
| non-zero | Validation, endpoint resolution or the request failed. |
