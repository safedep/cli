# safedep project show

Show one project by its exact name: its source, its labels from every source, and
the policy revision and policy overrides that apply to it.

## Synopsis

```
safedep project show PROJECT_NAME [--output table|plain|json]
```

## Examples

```bash
safedep project show PAY/settlement
```

```
Project          PAY/settlement
ID               3f1c...
Source           bitbucket · https://bitbucket.org/acme/settlement
Labels           safedep.io/project-source=bitbucket
                 scm/forge=bitbucket
                 scm/owner=acme
Policy           revision 46 · no override matches · base only
Vulnerabilities  0
Violations       -
Created          3d ago

Policy of each field: safedep project policy show PAY/settlement
```

When the policy service does not answer, the command warns and shows the rest.
When two projects have the name, the command lists their ids.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The project was read. |
| non-zero | The project does not exist, the name is ambiguous, or the request failed. |
