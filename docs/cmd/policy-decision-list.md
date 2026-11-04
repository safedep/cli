# safedep policy decision list

List the decisions that pmg and the pull request check made with the policy of
the tenant, newest first. Each row says which policy revision and which package
rule or field decided, and where the decision happened. A decision that did not
use the cloud policy is not in the list.

## Synopsis

```
safedep policy decision list [--since DURATION] [--until DURATION]
                             [--tool pmg|pull-request-check|vet]
                             [--outcome blocked|failed|allowed]
                             [--revision N] [--rule RULE_ID] [--override NAME|ID]
                             [--path PATH] [--package PURL]
                             [--project PROJECT_ID] [--endpoint ENDPOINT_ID]
                             [--limit N] [--page-token TOKEN] [--output table|plain|json]
```

## Flags

| Flag | Description |
|------|-------------|
| `--since <duration>` | Only decisions in this time before now, as `7d` or `12h`. |
| `--until <duration>` | Only decisions older than this time before now, as `1d`. It must be shorter than `--since`. |
| `--tool <tool>` | Only decisions of `pmg`, `pull-request-check` or `vet`. |
| `--outcome <outcome>` | Only decisions that `blocked` an install, `failed` a check, or `allowed` a package because a trust entry waived a verdict. |
| `--revision <n>` | Only decisions that applied this tenant policy revision. |
| `--rule <rule-id>` | Only decisions that this package rule made. |
| `--override <name\|id>` | Only decisions that this policy override made: a field that the override set, or a package rule that attaches to it. The command finds the name in the policy at `--revision`, or at head. |
| `--path <path>` | Only decisions that this field made, by its contract path, as `package_admission.cooldown.min_age`. |
| `--package <purl>` | Only decisions about this package, in all its versions. Give a PURL with no version, as `pkg:npm/lodash`. |
| `--project <id>` | Only decisions at a pull request or a scan of a repository that is bound to this project. |
| `--endpoint <id>` | Only decisions that pmg made on this endpoint. |
| `--limit <n>` | Page size. The server default applies when 0. |
| `--page-token <token>` | Continuation token from a prior response. |

Inherits root flags `--output` and `--profile`.

## Columns

| Column | Content |
|--------|---------|
| `WHEN` | When the tool made the decision. |
| `TOOL` | `pmg`, `pull-request-check` or `vet`. |
| `PACKAGE` | The ecosystem, the name and the version. |
| `OUTCOME` | `blocked`, `failed` or `allowed`. |
| `DECIDED BY` | The package rule, or the field path, and the layer or the override that set it: `default policy`, `base` or `override NAME`. |
| `REV` | The tenant policy revision. When the default policy decided, also its revision, as `47 · default 12`. |
| `WHERE` | For pmg, the hostname and the user, and one hint: the CI repository, else the Kubernetes namespace and workload, else the agent. For a check, the repository and the pull request number. For a scan, the repository and the branch or the commit. |
| `LINK` | The check run, the pull request or the CI run. The table has this column only when a row has a link. |

## Examples

The decisions that one package rule made:

```bash
safedep policy decision list --rule r_9c1x4kq7dm
```

```
Policy decisions
WHEN    TOOL                PACKAGE         OUTCOME  DECIDED BY         REV  WHERE                 LINK
5m ago  pmg                 npm/evil@1.0.0  blocked  rule r_9c1x4kq7dm  47   dev-laptop (alice)    -
5m ago  pull-request-check  npm/evil@1.0.1  failed   rule r_9c1x4kq7dm  47   acme/payments-api#42  https://github.com/acme/payments-api/runs/7
2 decisions
```

The installs that the cooldown blocked in the last day:

```bash
safedep policy decision list --path package_admission.cooldown.min_age --since 1d
```

```
Policy decisions
WHEN    TOOL  PACKAGE          OUTCOME  DECIDED BY                                           REV              WHERE
5m ago  pmg   npm/fresh@0.0.1  blocked  package_admission.cooldown.min_age · default policy  47 · default 12  dev-laptop (alice)
1 decision
```

The decisions of a policy override at one revision:

```bash
safedep policy decision list --override payments --revision 47
```

## Output

`json` holds `decisions`, the `PolicyDecision` messages as the server sends them
in protojson with the field names of the contract, and `next_page_token`. Each
message has the full citation (revisions, digest, path, rule id, override id,
layer) and the full decision point, such as the working directory, the CI branch
and commit, the Kubernetes Pod and the agent of a pmg run. `plain` writes a
tab-separated header and one row for each decision, with exact times and ids.

## Permissions

The roles that can read the policy can read the decision log. The tenant needs
policy management.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The page was read, also when it is empty. |
| non-zero | Validation, authentication or the request failed. |
