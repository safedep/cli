# Setup MCP Onboarding Design

**Date:** 2026-05-15
**Issues:** safedep/control-tower#787, safedep/control-tower#789
**Branch:** feat/setup-mcp-install-onboarding

## Problem

Two gaps exist in the CLI:

1. `safedep auth login` errors when the authenticated user has no tenant (`"contact SafeDep support"`). First-time users cannot self-register.
2. There is no `safedep setup mcp install` shortcut command that composes authentication, onboarding, and MCP config injection into a single first-timer flow.

## Scope

CLI-only for this iteration. Device flow (OAuth2 device authorization grant) is kept as the auth mechanism. A `--web` path (PKCE loopback redirect + app.safedep.io onboarding) is a future follow-up tracked separately; the code is structured so this seam is clean to add later.

## Design

### Part 1: Auth Login Onboarding (issue #787)

#### Current behaviour

`PostOAuthBootstrap` in `internal/auth/bootstrap.go` returns a hard error when `GetUserInfo` returns zero tenants.

#### New behaviour

When `len(tenants) == 0`, call `RegisterTenant` (new function, see below) before continuing. After successful registration, re-fetch tenants and proceed as normal.

#### New package: `internal/auth/register.go`

```go
RegisterTenant(ctx, input RegisterTenantInput) (tenantDomain string, error)
```

`RegisterTenantInput`:

- `AccessToken string`
- `Name string`
- `OrganizationName string`
- `OrganizationDomain string`
- `ConnFor ControlPlaneConnFunc` (optional, nil uses default)

Calls `OnboardingService.OnboardUser` with:

- `email`: extracted from JWT (`EmailFromAccessToken`), shown to user as editable default. Falls back to prompting if not in JWT. Server reads the real email from the API Gateway JWT header in production; the request-body field is a fallback for local/self-hosted environments without the gateway.
- `name`, `organization_name`, `organization_domain`: from caller

Collision handling: if `OnboardUser` returns a domain-uniqueness error, caller regenerates suffix and retries up to 3 times. On the fourth failure, return a user-facing error: `"could not find an available domain after 3 attempts — try a different organization name"`.

`ConnFor` follows the same test-injection pattern as `BootstrapInput.ConnFor`. Tests inject a fake to exercise: success, collision-and-retry, exhausted retries, and validation errors without hitting the network.

This function is the seam for the future web path. A future `RegisterTenantViaWeb` can satisfy the same interface.

#### New helper: `internal/auth/jwt.go`

```go
EmailFromAccessToken(token string) (string, error)
```

Decodes (unverified, same as `AccessTokenExpiry`) the JWT access token and extracts the `email` claim. Also tries the namespaced `https://safedep.io/email` claim as fallback. Returns empty string if neither is present — callers treat empty as "not available" and leave the input field blank for the user to fill in.

#### New package: `internal/auth/domain.go`

Faithful Go port of `normalizeTenantDomain` and `buildGeneratedTenantName` from `app.safedep.io/src/app/onboard/_lib/generate-tenant-domain.ts`:

- Lowercases input
- Strips unicode accents (NFD decomposition)
- Replaces non-alphanumeric characters with hyphens
- Collapses repeated hyphens
- Trims leading/trailing hyphens
- Truncates slug to leave room for the random suffix
- Appends 4-char random hex suffix
- Caps total length at 63 characters

```go
NormalizeTenantDomain(s string) string
GenerateTenantDomain(orgName string) string
```

#### Prompt sequence in `cmd/auth/login.go`

Added to `runDeviceLogin` only when `PostOAuthBootstrap` signals zero-tenant. Extracted as `promptRegistration(a *app.App, accessToken string) (registrationInput, error)`:

1. Email — pre-filled from `EmailFromAccessToken` if available; validated as a syntactically valid email address after trim. Re-prompts until valid.
2. "Your name" — required, min 1 char after trim. Re-prompts until valid.
3. "Organization name" — required, min 1 char after trim. Re-prompts until valid.
4. "Domain" — default from `GenerateTenantDomain(orgName)`, shown to user, editable. Re-prompts until valid.

All fields are trimmed before validation. No gRPC call is made until all fields pass local validation.

#### Disallowed combinations

If `--no-api-key` is passed and registration flow triggers (zero tenants), return an error:
`"--no-api-key cannot be used during initial registration: an API key is required to complete setup"`

#### `--web` hook (deferred)

The code path for web-based login is commented out in `runDeviceLogin` as a placeholder. No flag is exposed. Comment references the future design.

---

### Part 2: Setup MCP Install (issue #789)

#### New domain: `internal/cmd/setup/`

```text
internal/cmd/setup/
  cmd.go         -- Register(root, app) + "setup" parent command
  mcp/
    cmd.go       -- Register(parent, app) + "mcp" sub-command
    install.go   -- "install" leaf command; accepts --force flag
```

`safedep.go` gains `setup.Register(root, a)`.

#### `setup mcp install` behaviour

Checks for existing valid credentials first:

```text
if credentials exist (APIKeyResolver succeeds and returns a tenant) AND --force not passed:
    skip device flow
    reuse existing API key + tenant for MCP injection

else (first-timer or --force):
    run device flow + PostOAuthBootstrap (triggers registration if zero tenants)
    always create a new API key during bootstrap
    save credentials via SaveBootstrapResult
    use new credentials for MCP injection
```

Then in both paths:

- `endpoint.BuildMCPConfig` (builds config with Authorization, X-Tenant-ID, X-Endpoint-ID headers)
- `agent.InjectAll` (injects into all detected AI agents)

**API key on re-run:** when the full flow runs (first-timer or `--force`), a new API key is always created. Keys are named with `APIKeyName(hostname(), time.Now())` and carry the default expiry (`DefaultAPIKeyExpiryDays`). Prior keys with the same hostname pattern are not revoked automatically; they expire per the default TTL. This is a known trade-off — the same behaviour as vet's quickstart.

`--force` flag is defined on `installCmd` in `cmd/setup/mcp/install.go`.

#### New helper: `internal/auth/save.go`

```go
SaveBootstrapResult(store cloud.CredentialStore, accessToken, refreshToken string, b *BootstrapResult) error
```

Extracted from `runDeviceLogin` in `cmd/auth/login.go`. Saves access token, refresh token, and (if present) API key to the keychain. Both `cmd/auth/login.go` and `cmd/setup/mcp/install.go` call this instead of duplicating the credential-saving logic.

#### Failure semantics

- Auth failure: surface error, stop. Nothing is saved.
- Auth succeeds, MCP injection fails (no agents detected, write-protected config, etc.): credentials ARE saved. Message: `"credentials saved. MCP configuration failed: <error>. Fix the issue and run 'safedep protect mcp install' to retry."`
- Auth succeeds, injection succeeds: single success message listing each configured agent.

---

## File map

| File | Change |
| --- | --- |
| `internal/auth/bootstrap.go` | Add zero-tenant branch calling `RegisterTenant` |
| `internal/auth/register.go` | New: `RegisterTenant`, `RegisterTenantInput` |
| `internal/auth/domain.go` | New: `NormalizeTenantDomain`, `GenerateTenantDomain` |
| `internal/auth/jwt.go` | Add `EmailFromAccessToken` |
| `internal/auth/save.go` | New: `SaveBootstrapResult` |
| `internal/cmd/auth/login.go` | Add `promptRegistration`; call `SaveBootstrapResult`; guard `--no-api-key` |
| `internal/cmd/setup/cmd.go` | New |
| `internal/cmd/setup/mcp/cmd.go` | New |
| `internal/cmd/setup/mcp/install.go` | New; defines `--force` flag |
| `internal/cmd/safedep.go` | Register setup domain |

## Dependencies

No new external dependencies. All gRPC clients (`OnboardingService`) are already in the `buf.build` generated package.

## Done criteria

- Fresh account: `safedep auth login` completes the registration prompts, creates a tenant and API key, stores credentials. Subsequent `auth status` shows the tenant.
- Authenticated account, no MCP configured: `safedep setup mcp install` skips device flow, injects MCP config into all detected agents, reports which agents were configured.
- Fresh machine, no credentials: `safedep setup mcp install` runs the full device flow, registers tenant if needed, creates API key, injects MCP config — one command, end to end.
- Re-run with `--force`: re-authenticates, creates a new API key, re-injects MCP config.

## Not in scope

- `--web` flag / PKCE loopback redirect path (future; seam is in place)
- npm packaging (`pnpx @safedep/cli`) — no issue yet
- DCR enablement on auth.safedep.io — tracked in MCP OAuth Notion doc
- `safedep setup guard install` — future command, same pattern
