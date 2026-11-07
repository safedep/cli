# safedep policy rule create

Create a package rule and publish it at once as a new policy revision. A block
entry stops new installs of the packages that it selects. A trust entry waives
the verdicts of its trust scopes for one package, but never a verdict of
verified malware.

## Synopsis

```
safedep policy rule create --block PURL [--vers RANGE] --reason TEXT [flags]
safedep policy rule create --block -f FILE --reason TEXT [flags]
safedep policy rule create --trust PURL --scope SCOPE --expires DURATION --reason TEXT [flags]
safedep policy rule create --trust PURL --scope vulnerability --vulnerability ID[:JUSTIFICATION]... --expires DURATION --reason TEXT [flags]
```

## Flags

| Flag | Description |
|------|-------------|
| `--block` | Create block entries. |
| `--trust` | Create a trust entry. |
| `-f`, `--file <path>` | With `--block`: a file with one selector on each line, a PURL and an optional range after a space. `#` starts a comment. `-` reads stdin. At most 1,000 selectors, in one revision. |
| `--vers <range>` | A version range, as `'>=1.2.0'` or `'vers:npm/>=1.2.0\|<2.0.0'`. A bare range gets the `vers:` prefix and the type of the PURL. |
| `--scope <scope>` | With `--trust`: `malware`, `cooldown`, `vulnerability`, `license` or `all`. Repeatable. Required. |
| `--vulnerability <id>[:<justification>]` | With `--scope vulnerability` as the only scope: waive only this vulnerability id, as `CVE-2024-3094`. Repeatable, at most 100 ids, each one time. See below. |
| `--expires <when>` | A duration from now, as `30d`, or a date, as `2027-01-26`. Required with `--trust`, and at most 90 days from now for a trust entry. |
| `--reason <text>` | Why the rule exists. Required. Tools show it in a block message. |
| `--ticket <ref>` | A ticket or incident reference, as `IR-2291`. |
| `--requested-by <who>` | The team or person that asked for the rule. |
| `--override <name>` | Attach the rule to this policy override. The policy base when empty. |
| `-m`, `--message <text>` | The publish message. The server writes one when it is empty. |

Selector rules, which the CLI checks before the request:

- A selector is a PURL, as `pkg:npm/evil` or `pkg:pypi/foo@1.0.3`.
- Only a block entry can use a prefix. A prefix ends with `/*` or `.*`, as
  `pkg:npm/@acme/*`, `pkg:maven/org.apache.*` or `pkg:nuget/Microsoft.*`. A prefix
  has no version, qualifiers or subpath. Use `--vers` for a range.
- A trust entry selects one package.
- Set a version in the PURL or a `--vers` range, not both.
- A prefix such as `pkg:npm/*` selects every package of an ecosystem. The command
  warns, because such a block stops every install of that ecosystem in the tenant.

Trust scopes:

| Scope | Waives |
|-------|--------|
| `malware` | A malware verdict that is not verified. |
| `cooldown` | The dependency cooldown. |
| `vulnerability` | Every vulnerability finding of a scan gate, also one that is published later. With `--vulnerability`, only the named vulnerabilities. |
| `license` | Every license finding of a scan gate: a denied license, a license outside the allow list and an unknown license. |

`--scope all` expands to every trust scope that this CLI knows. The contract has
no "all" value, so a scope that a later release adds never widens a stored entry.
`all` never waives named vulnerabilities, because that scope needs ids.

`--scope vulnerability` with one or more `--vulnerability` flags creates a trust
entry with the one scope `named-vulnerability`. It waives only the vulnerabilities
that it names and their aliases, so a CVE id also waives the GHSA id of the same
vulnerability. A scan gate still fails on every other vulnerability of the
package. The command refuses `--vulnerability` with another scope or with `all`.

Write the id as its advisory publishes it, as `CVE-2024-3094`,
`GHSA-jfh8-c2jp-5v3q` or `RHSA-2024:1234`. The optional justification says why
the vulnerability does not affect the package, with an OpenVEX name:

- `component_not_present`
- `vulnerable_code_not_present`
- `vulnerable_code_not_in_execute_path`
- `vulnerable_code_cannot_be_controlled_by_adversary`
- `inline_mitigations_already_exist`

An id can hold a `:`. The command splits the value at its last `:` only when the
text after it is a justification name. Otherwise the whole value is the id, so
`RHSA-2024:1234` is one id. Without a justification, `--reason` is the only
explanation.

## Examples

Block a malicious package at 2am:

```bash
safedep policy rule create --block pkg:npm/evil --vers '>=1.2.0' \
  --reason "IR-2291 credential stealer" --ticket IR-2291
```

```
Applies to every endpoint and project of the tenant (base). Use --override NAME to narrow it.
A block stops new installs. It does not remove installed copies.
✓ rule r_9c1 created · revision 48 published
  Each endpoint applies it after its next policy fetch. Pull request checks apply it at once.
  Endpoints that have not applied it yet: safedep endpoint list --applied-revision '<48'
```

Trust a false positive:

```bash
safedep policy rule create --trust pkg:pypi/foo@1.0.3 --scope malware \
  --reason "False positive, S-88" --expires 30d --requested-by data-eng
```

```
Applies to every endpoint and project of the tenant (base). Use --override NAME to narrow it.
! This rule loosens the policy: it waives a malware verdict that is not verified for pkg:pypi/foo@1.0.3.
! Verified malware stays blocked
✓ rule r_a41 created · revision 49 published
  The rule expires in 30d (2026-11-30)
```

Waive two vulnerabilities of one version, the first with a justification:

```bash
safedep policy rule create --trust pkg:npm/foo@1.2.3 --scope vulnerability \
  --vulnerability CVE-2024-1234:vulnerable_code_not_in_execute_path \
  --vulnerability GHSA-jfh8-c2jp-5v3q --reason "Not reachable, S-90" --expires 30d
```

```
Applies to every endpoint and project of the tenant (base). Use --override NAME to narrow it.
! This rule loosens the policy: it waives the scan gate findings of CVE-2024-1234, GHSA-jfh8-c2jp-5v3q for pkg:npm/foo@1.2.3.
A scan gate still fails on every other vulnerability of the package.
! Verified malware stays blocked
✓ rule r_5d2 created · revision 50 published
  The rule expires in 30d (2026-12-07)
```

Waive the license findings of one version:

```bash
safedep policy rule create --trust pkg:npm/bar@2.0.0 --scope license \
  --reason "Legal approved the dual license, LEG-12" --expires 90d
```

Block a list of packages in one revision:

```bash
safedep policy rule create --block -f iocs.txt --reason "IR-2300 campaign"
```

## Permissions

In the MVP every rule change needs the Admin role.

## Authentication

Requires a control-plane OAuth session. Run `safedep auth login` first.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | The rules were created and the revision was published. |
| non-zero | Validation, authentication or the request failed. Nothing was published. |
