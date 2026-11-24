# safedep workload rule show

Show one workload trust rule by its id or its name.

## Synopsis

```
safedep workload rule show RULE [--output table|plain|json]
```

## Examples

```bash
safedep workload rule show web
```

```
Rule          wtr_9d1
Name          web
Provider      github-actions:acme
Grant         tool
Repositories  acme/web (123456)
Conditions    ref matches refs/heads/release/*
Projects      manual: a repository with no project needs a project before a tool session
Max lifetime  6h
Created       10m ago
Last used     never
```

`Repositories` shows each repository with its numeric id, which the rule
matches. A rule with no repository shows `every repository of acme`. `Projects`
says how a repository with no project gets a project at its first tool session.
`Max lifetime` is the longest life of a credential that the rule gives.

## Output

`json` holds `rule`: rule id, name, provider name and id, grant, `repositories`
(`repository_id` and `full_name`), `every_repository`, `conditions` (claim,
operator and values, the `repository_id` condition included), project binding,
max lifetime, creation time and last use. `plain` writes one tab-separated key
and value on each line.

## Permissions

Needs the Admin role.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The rule was read. |
| non-zero | The rule does not exist, or the request failed. |
