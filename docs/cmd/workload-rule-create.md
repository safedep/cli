# safedep workload rule create

Create a workload trust rule. The rule gives one credential grant to the CI jobs
of one provider whose claims meet every condition of the rule. A job of a GitHub
Actions workflow then calls SafeDep with no API key in the CI secrets.

## Synopsis

```
safedep workload rule create NAME --provider PROVIDER --grant GRANT
                             [--repository OWNER/NAME...] [--ref REF...] [--event EVENT...]
                             [--project-binding auto|manual] [--max-lifetime DURATION]
                             [--output table|plain|json]
```

`NAME` is unique in the tenant: 1 to 63 lowercase letters, digits and hyphens,
as in `org` or `web-release`.

## Flags

| Flag | Description |
|------|-------------|
| `--provider <provider>` | The provider of the rule: a provider id or `KIND:ACCOUNT`, as in `github-actions:acme`. Required. |
| `--grant <grant>` | The grant that the rule gives. `tool` lets the job call the RPCs that pmg, vet and the `safedep` CLI use. Required. |
| `--repository <owner/name>` | A repository by its full name, as in `acme/web`. Repeat the flag, or separate names with commas. At most 100 repositories. |
| `--ref <ref>` | A full ref, as in `refs/heads/main`, or a prefix that ends with `/*`, as in `refs/heads/release/*`. Repeat the flag for more refs. |
| `--event <event>` | An event name of the platform, as in `push` or `pull_request_target`. Repeat the flag for more events. |
| `--project-binding <binding>` | How a repository with no project gets a project: `auto` or `manual`. See [Projects](#projects). |
| `--max-lifetime <duration>` | The longest life of a credential that the rule gives, from `1m` to `6h`, as in `30m`. The server default is `6h`. |

## The provider is required

The CLI never picks a provider, even when the tenant has one provider. With no
`--provider`, the command fails and lists the providers of the tenant:

```
--provider is required. The CLI never picks a provider. The providers of the tenant:
  github-actions:acme  id wip_7a1, link active
Run the command again with --provider github-actions:acme
```

## Repositories

The server finds each `--repository` in the account of the provider, through the
installation of the SafeDep GitHub App, and stores its numeric id. The rule
matches the id, so a rename of the repository does not change the rule. The
server refuses a repository that the installation cannot see:

```
the GitHub installation of the provider cannot see a repository
other/web is not a repository of the provider account that the installation can see
```

A rule with no `--repository` covers every repository of the account, also the
repositories that the installation does not select. The command says so when
it creates such a rule.

## Conditions

- `--ref` takes full refs or prefixes, not both. A prefix ends at a `/`, so
  `refs/heads/main*` is refused: it would also match `refs/heads/main-evil`.
- A rule with no `--event` covers only the default events of the provider. For
  GitHub Actions they are `push`, `pull_request`, `merge_group`, `schedule`,
  `workflow_dispatch` and `release`. Any other event, such as `issues` or
  `pull_request_target`, matches only a rule that lists it with `--event`.

## Projects

`--project-binding` says how a repository with no project gets a project:

- `auto`: the first tool session of the repository creates its project.
- `manual`: a member adds the repository as a project first. A tool session
  from a repository with no project fails. Policy fetch and event sync still work.

With no `--project-binding`, the server picks: `auto` for a rule with no
repository, and `manual` for a rule with repositories.

## Examples

One rule for every repository of the organization:

```bash
safedep workload rule create org --provider github-actions:acme --grant tool
```

One rule for the release branches of one repository:

```bash
safedep workload rule create web-release --provider github-actions:acme --grant tool \
  --repository acme/web --ref 'refs/heads/release/*'
```

```
✓ rule web-release created
Rule          wtr_9d1
Name          web-release
Provider      github-actions:acme
Grant         tool
Repositories  acme/web (123456)
Conditions    ref matches refs/heads/release/*
Projects      manual: a repository with no project needs a project before a tool session
Max lifetime  6h
Created       now
Last used     never
```

## Output

The command prints the rule as [`safedep workload rule show`](./workload-rule-show.md)
does. `json` holds `rule`, with each repository as `repository_id` and
`full_name`, and `every_repository`.

## Permissions

Needs the Admin role. The server records an audit event.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The rule was created. |
| non-zero | Validation failed, the provider is missing or unknown, the server refused a repository, or the request failed. |
