# safedep audit event list

List the audit events of the tenant, newest first. Each admin action, such as a
policy publish or a label change, writes one audit event.

## Synopsis

```
safedep audit event list [--since DURATION] [--until DURATION] [--action PREFIX]
                         [--resource KIND[/ID]] [--actor ACTOR]
                         [--limit N] [--page-token TOKEN] [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--since <duration>` | Only events in this time before now, as `7d` or `12h`. |
| `--until <duration>` | Only events older than this time before now, as `1d`. It must be shorter than `--since`. |
| `--action <prefix>` | Only events whose action starts with this text. `policy.` lists every policy action, and `policy.rule.` lists the package rule actions. |
| `--resource <kind[/id]>` | Only events about a resource kind, or one resource. The kinds are `policy_change`, `package_rule`, `endpoint` and `project`, as in `--resource package_rule` or `--resource policy_change/18`. |
| `--actor <actor>` | Only events of this actor. The server matches the actor id or the display name exactly. |
| `--limit <n>` | Page size. The server default applies when 0. |
| `--page-token <token>` | Continuation token from a prior response. |

Actions have the form `<domain>.<resource>.<verb>`, as `policy.change.publish`.
To list the policy events, use `--action policy.`. `--resource` takes a resource
kind, not a domain.

## Examples

```bash
safedep audit event list --since 7d --action policy.
```

```
Audit events
WHEN     ACTOR  ACTION                 RESOURCE            RESULT   DETAIL
10m ago  alice  policy.rule.delete     package_rule r_80b  success  revision=51
12m ago  alice  policy.rule.update     package_rule r_77e  success  revision=50
2d ago   alice  policy.change.publish  policy_change 18    success  revision=47
3 events
```

## Output

`json` holds `events` (event id, actor, actor kind, action, resource kind and id,
result, details, request id, time) and `next_page_token`. `plain` writes a
tab-separated header and one row for each event.

## Permissions

Reading the audit log needs the Admin role.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was read. |
| non-zero | Validation, authentication or the request failed. |
