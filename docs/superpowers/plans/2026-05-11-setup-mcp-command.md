# Setup MCP Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `safedep setup mcp install` and `safedep setup mcp uninstall` — commands that detect AI agents on the machine and inject or remove the SafeDep MCP server configuration from each agent's config files.

**Architecture:** A `setup` domain package hosts the `mcp` sub-domain package, following the `protect/mcp` pattern from DEVGUIDE. A shared `mcpService` struct handles both install and uninstall orchestration; `RunE` in each leaf resolves credentials from `App` and wires the service. `endpoint.BuildMCPConfig` assembles the MCP config with all three SafeDep headers; `agent.InjectAll`/`RemoveAll` fan it out to detected agents.

**Tech Stack:** Go, cobra/spf13, `internal/agent`, `internal/endpoint`, `dry/cloud/endpointsync`, `dry/tui`, testify.

---

## File Map

| File | Responsibility |
|---|---|
| `internal/cmd/setup/cmd.go` | Register + `setup` parent cobra command |
| `internal/cmd/setup/mcp/cmd.go` | `mcp` parent cobra command + `Register(parent, a)` |
| `internal/cmd/setup/mcp/service.go` | `mcpService`, `installInput`, `uninstallInput`, shared orchestration |
| `internal/cmd/setup/mcp/install.go` | `install` leaf command; wires credentials → service |
| `internal/cmd/setup/mcp/uninstall.go` | `uninstall` leaf command; wires service |
| `internal/cmd/setup/mcp/service_test.go` | Service tests with fake agents and fake resolver |
| `internal/cmd/safedep.go` | Add `setup.Register(root, a)` |
| `docs/cmd/setup-mcp-install.md` | Doc page (required by convention lint) |
| `docs/cmd/setup-mcp-uninstall.md` | Doc page (required by convention lint) |
| `README.md` | Add two rows to the commands table (required by convention lint) |

---

## Task 1: Service (orchestration core)

**Files:**
- Create: `internal/cmd/setup/mcp/service.go`
- Create: `internal/cmd/setup/mcp/service_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cmd/setup/mcp/service_test.go
package mcp

import (
	"errors"
	"testing"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/safedep/cli/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes ---

type fakeResolver struct {
	identity *controltowerv1.EndpointIdentity
	err      error
}

func (f *fakeResolver) Resolve() (*controltowerv1.EndpointIdentity, error) {
	return f.identity, f.err
}

type fakeGlobalInjector struct {
	injected *agent.MCPConfig
	removed  bool
	injectErr error
}

func (f *fakeGlobalInjector) GlobalConfigPath() string { return "/fake/path" }
func (f *fakeGlobalInjector) InjectGlobal(cfg agent.MCPConfig) error {
	if f.injectErr != nil {
		return f.injectErr
	}
	c := cfg
	f.injected = &c
	return nil
}
func (f *fakeGlobalInjector) RemoveGlobal() error {
	f.removed = true
	return nil
}

type fakeAgent struct {
	name     string
	detected bool
	global   *fakeGlobalInjector
}

func (f *fakeAgent) Name() string    { return f.name }
func (f *fakeAgent) Detected() bool  { return f.detected }
func (f *fakeAgent) AsGlobalInjector() (agent.GlobalInjector, bool) {
	if f.global == nil {
		return nil, false
	}
	return f.global, true
}
func (f *fakeAgent) AsWorkspaceInjector() (agent.WorkspaceInjector, bool) {
	return nil, false
}

// --- tests ---

var testIdentity = &controltowerv1.EndpointIdentity{
	Identifier: "test-host",
	MachineId:  "test-machine-id",
}

func TestMCPServiceInstall(t *testing.T) {
	t.Run("injects into detected agent with correct headers", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "claude-code", detected: true, global: gi}
		svc := newMCPService([]agent.Agent{a}, &fakeResolver{identity: testIdentity})

		require.NoError(t, svc.install(installInput{
			MCPURL:   "https://mcp.safedep.io/v1",
			APIKey:   "key123",
			TenantID: "tenant-1",
		}))
		require.NotNil(t, gi.injected)
		assert.Equal(t, "https://mcp.safedep.io/v1", gi.injected.URL)
		assert.Equal(t, "Bearer key123", gi.injected.Headers["Authorization"])
		assert.Equal(t, "tenant-1", gi.injected.Headers["X-Tenant-ID"])
		assert.NotEmpty(t, gi.injected.Headers["X-Endpoint-ID"])
	})

	t.Run("skips undetected agents", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "cursor", detected: false, global: gi}
		svc := newMCPService([]agent.Agent{a}, &fakeResolver{identity: testIdentity})

		require.NoError(t, svc.install(installInput{
			MCPURL:   "https://mcp.safedep.io/v1",
			APIKey:   "k",
			TenantID: "t",
		}))
		assert.Nil(t, gi.injected)
	})

	t.Run("returns error when identity resolver fails", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "claude-code", detected: true, global: gi}
		svc := newMCPService([]agent.Agent{a}, &fakeResolver{err: errors.New("no machine id")})

		err := svc.install(installInput{
			MCPURL:   "https://mcp.safedep.io/v1",
			APIKey:   "k",
			TenantID: "t",
		})
		require.Error(t, err)
		assert.Nil(t, gi.injected)
	})

	t.Run("no detected agents returns nil without error", func(t *testing.T) {
		svc := newMCPService([]agent.Agent{}, &fakeResolver{identity: testIdentity})
		require.NoError(t, svc.install(installInput{
			MCPURL:   "https://mcp.safedep.io/v1",
			APIKey:   "k",
			TenantID: "t",
		}))
	})
}

func TestMCPServiceUninstall(t *testing.T) {
	t.Run("removes from detected agent", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "claude-code", detected: true, global: gi}
		svc := newMCPService([]agent.Agent{a}, nil)

		require.NoError(t, svc.uninstall(uninstallInput{}))
		assert.True(t, gi.removed)
	})

	t.Run("skips undetected agents", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "cursor", detected: false, global: gi}
		svc := newMCPService([]agent.Agent{a}, nil)

		require.NoError(t, svc.uninstall(uninstallInput{}))
		assert.False(t, gi.removed)
	})

	t.Run("no detected agents returns nil without error", func(t *testing.T) {
		svc := newMCPService([]agent.Agent{}, nil)
		require.NoError(t, svc.uninstall(uninstallInput{}))
	})
}
```

- [ ] **Step 2: Run to confirm failure (undefined types expected)**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/cmd/setup/... 2>&1 | head -10
```

Expected: compilation errors (`newMCPService`, `installInput`, `uninstallInput` undefined).

- [ ] **Step 3: Write `service.go`**

```go
// internal/cmd/setup/mcp/service.go
package mcp

import (
	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/cli/internal/endpoint"
	"github.com/safedep/dry/cloud/endpointsync"
	"github.com/safedep/dry/tui"
)

type installInput struct {
	MCPURL       string
	APIKey       string
	TenantID     string
	WorkspaceDir string
}

type uninstallInput struct {
	WorkspaceDir string
}

type mcpService struct {
	agents   []agent.Agent
	resolver endpointsync.EndpointIdentityResolver
}

func newMCPService(agents []agent.Agent, resolver endpointsync.EndpointIdentityResolver) *mcpService {
	return &mcpService{agents: agents, resolver: resolver}
}

func (s *mcpService) install(in installInput) error {
	cfg, err := endpoint.BuildMCPConfig(in.MCPURL, in.APIKey, in.TenantID, s.resolver)
	if err != nil {
		return err
	}

	detected := s.detectedAgents()
	if len(detected) == 0 {
		tui.Warning("No supported AI agents detected on this machine.")
		return nil
	}

	for _, a := range detected {
		tui.Info("Configuring %s", a.Name())
	}

	if err := agent.InjectAll(detected, cfg, in.WorkspaceDir); err != nil {
		return err
	}

	tui.Success("SafeDep MCP server configured for %d agent(s).", len(detected))
	return nil
}

func (s *mcpService) uninstall(in uninstallInput) error {
	detected := s.detectedAgents()
	if len(detected) == 0 {
		tui.Warning("No supported AI agents detected on this machine.")
		return nil
	}

	if err := agent.RemoveAll(detected, in.WorkspaceDir); err != nil {
		return err
	}

	tui.Success("SafeDep MCP server configuration removed from %d agent(s).", len(detected))
	return nil
}

func (s *mcpService) detectedAgents() []agent.Agent {
	var detected []agent.Agent
	for _, a := range s.agents {
		if a.Detected() {
			detected = append(detected, a)
		}
	}
	return detected
}
```

- [ ] **Step 4: Run service tests**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/cmd/setup/mcp/... -run "TestMCPServiceInstall|TestMCPServiceUninstall" -v 2>&1
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/cmd/setup/mcp/service.go internal/cmd/setup/mcp/service_test.go
git commit -m "feat(setup/mcp): add mcpService with install and uninstall orchestration"
```

---

## Task 2: Leaf commands

**Files:**
- Create: `internal/cmd/setup/mcp/install.go`
- Create: `internal/cmd/setup/mcp/uninstall.go`

- [ ] **Step 1: Write `install.go`**

```go
// internal/cmd/setup/mcp/install.go
package mcp

import (
	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/cloud/endpointsync"
	"github.com/spf13/cobra"
)

const defaultMCPServerURL = "https://mcp.safedep.io/model-context-protocol/threats/v1"

type installFlags struct {
	MCPURL       string
	WorkspaceDir string
}

func installCmd(a *app.App) *cobra.Command {
	var flags installFlags

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install SafeDep MCP server configuration into AI agents",
		Long: "Detect AI coding agents installed on this machine and inject the SafeDep MCP " +
			"server entry into each agent's config file. Requires an authenticated session " +
			"(`safedep auth login`). Pass --workspace to also inject into the current project.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolver, err := a.APIKeyResolver()
			if err != nil {
				return err
			}

			creds, err := resolver.Resolve()
			if err != nil {
				return err
			}

			apiKey, err := creds.GetAPIKey()
			if err != nil {
				return err
			}

			tenantID, err := creds.GetTenantDomain()
			if err != nil {
				return err
			}

			svc := newMCPService(agent.NewRegistry(), endpointsync.NewEndpointIdentityResolver())

			return svc.install(installInput{
				MCPURL:       flags.MCPURL,
				APIKey:       apiKey,
				TenantID:     tenantID,
				WorkspaceDir: flags.WorkspaceDir,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&flags.MCPURL, "mcp-url", defaultMCPServerURL, "SafeDep MCP server URL")
	f.StringVar(&flags.WorkspaceDir, "workspace", "", "project directory for workspace-level injection (empty = skip)")

	return cmd
}
```

- [ ] **Step 2: Write `uninstall.go`**

```go
// internal/cmd/setup/mcp/uninstall.go
package mcp

import (
	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/cloud/endpointsync"
	"github.com/spf13/cobra"
)

type uninstallFlags struct {
	WorkspaceDir string
}

func uninstallCmd(a *app.App) *cobra.Command {
	var flags uninstallFlags

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove SafeDep MCP server configuration from AI agents",
		Long: "Remove the SafeDep MCP server entry from the configuration files of all AI " +
			"coding agents detected on this machine. Does not require authentication.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := newMCPService(agent.NewRegistry(), endpointsync.NewEndpointIdentityResolver())

			return svc.uninstall(uninstallInput{
				WorkspaceDir: flags.WorkspaceDir,
			})
		},
	}

	cmd.Flags().StringVar(&flags.WorkspaceDir, "workspace", "", "project directory for workspace-level removal (empty = skip)")

	return cmd
}
```

- [ ] **Step 3: Write `internal/cmd/setup/mcp/cmd.go`**

```go
// internal/cmd/setup/mcp/cmd.go
package mcp

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register adds the `mcp` sub-command tree to parent (the `setup` command).
func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage SafeDep MCP server configuration for AI agents",
		Long: "Install or remove the SafeDep MCP server from the configuration files of AI " +
			"coding agents (Claude Code, Cursor, Gemini CLI, and others) detected on this machine.",
	}
	cmd.AddCommand(installCmd(a))
	cmd.AddCommand(uninstallCmd(a))
	parent.AddCommand(cmd)
}
```

- [ ] **Step 4: Write `internal/cmd/setup/cmd.go`**

```go
// internal/cmd/setup/cmd.go
package setup

import (
	"github.com/safedep/cli/internal/app"
	setupmcp "github.com/safedep/cli/internal/cmd/setup/mcp"
	"github.com/spf13/cobra"
)

// Register wires the setup command tree onto root.
func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "setup",
		Short: "Set up SafeDep integrations and tooling",
		Long:  "Configure integrations with SafeDep Cloud, including AI agent MCP server installation.",
	}
	setupmcp.Register(parent, a)
	root.AddCommand(parent)
}
```

- [ ] **Step 5: Build to confirm it compiles**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go build ./internal/cmd/setup/... 2>&1
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/cmd/setup/
git commit -m "feat(setup/mcp): add install and uninstall leaf commands"
```

---

## Task 3: Wire into the root command tree

**Files:**
- Modify: `internal/cmd/safedep.go`

- [ ] **Step 1: Register setup in safedep.go**

Edit `internal/cmd/safedep.go`. Add the import and registration line:

```go
// internal/cmd/safedep.go
package cmd

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/auth"
	"github.com/safedep/cli/internal/cmd/endpoint"
	"github.com/safedep/cli/internal/cmd/integration"
	"github.com/safedep/cli/internal/cmd/query"
	"github.com/safedep/cli/internal/cmd/setup"
	"github.com/safedep/cli/internal/cmd/version"
	"github.com/spf13/cobra"
)

// NewSafedep assembles the full safedep command tree. main() and the
// convention tests both consume this so they walk an identical tree.
func NewSafedep(a *app.App) *cobra.Command {
	root := NewRootCommand(a)
	auth.Register(root, a)
	endpoint.Register(root, a)
	query.Register(root, a)
	integration.Register(root, a)
	setup.Register(root, a)
	version.Register(root, a)
	return root
}
```

- [ ] **Step 2: Run the convention tests to catch anything broken**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/cmd/... -run TestConventions -v 2>&1 | grep -E "FAIL|PASS|---"
```

Expected: `TestConventions_LeafDocPagesExist` and `TestConventions_ReadmeLinksAllDocPages` will FAIL because the doc pages and README links do not exist yet. All others should PASS. If any other test fails, fix it before continuing.

- [ ] **Step 3: Commit**

```bash
git add internal/cmd/safedep.go
git commit -m "feat(cmd): register setup command tree"
```

---

## Task 4: Doc pages and README

**Files:**
- Create: `docs/cmd/setup-mcp-install.md`
- Create: `docs/cmd/setup-mcp-uninstall.md`
- Modify: `README.md`

- [ ] **Step 1: Write `docs/cmd/setup-mcp-install.md`**

```markdown
# safedep setup mcp install

Detect AI coding agents installed on this machine and inject the SafeDep MCP server entry into each agent's config file.

Agents supported: Claude Code, Cursor, Gemini CLI, OpenCode, Antigravity.

## Synopsis

```
safedep setup mcp install [flags]
```

## Flags

| Flag | Description |
|---|---|
| `--mcp-url <url>` | SafeDep MCP server URL (default: `https://mcp.safedep.io/model-context-protocol/threats/v1`). |
| `--workspace <dir>` | Project directory for workspace-level injection. Empty (default) skips workspace injection. |

Inherits root flags `--output`, `--profile`, and `--insecure-keychain-fallback`.

## Authentication

Requires an authenticated session. Run `safedep auth login` first to store API key and tenant credentials.

## What it does

1. Resolves the active profile's API key and tenant from the keychain.
2. Computes the machine's stable endpoint identity (`X-Endpoint-ID`).
3. Detects which supported AI agents are installed on this machine.
4. Writes the `mcpServers.safedep` entry into each detected agent's config file. The write is idempotent — repeated calls overwrite only the `safedep` key.

## Exit codes

- `0` on success (including the case where no agents are detected).
- `1` on any error (missing credentials, filesystem failure, endpoint identity failure).
```

- [ ] **Step 2: Write `docs/cmd/setup-mcp-uninstall.md`**

```markdown
# safedep setup mcp uninstall

Remove the SafeDep MCP server entry from the configuration files of all AI coding agents detected on this machine.

## Synopsis

```
safedep setup mcp uninstall [flags]
```

## Flags

| Flag | Description |
|---|---|
| `--workspace <dir>` | Project directory for workspace-level removal. Empty (default) skips workspace. |

Inherits root flags `--output`, `--profile`, and `--insecure-keychain-fallback`.

## Authentication

Does not require authentication. The removal is a local filesystem operation.

## What it does

1. Detects which supported AI agents are installed on this machine.
2. Removes the `mcpServers.safedep` entry from each detected agent's config file. All other keys are preserved.

## Exit codes

- `0` on success (including the case where no agents are detected or the entry is already absent).
- `1` on filesystem errors.
```

- [ ] **Step 3: Add rows to README.md**

Find the commands table in `README.md` (the `| Command | Description |` section) and add these two rows after the existing `integration jfrog run` row:

```markdown
| [`safedep setup mcp install`](./docs/cmd/setup-mcp-install.md) | Inject SafeDep MCP server config into detected AI agents |
| [`safedep setup mcp uninstall`](./docs/cmd/setup-mcp-uninstall.md) | Remove SafeDep MCP server config from detected AI agents |
```

- [ ] **Step 4: Run convention tests — all should pass now**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/cmd/... -run TestConventions -v 2>&1 | grep -E "FAIL|PASS|---"
```

Expected: all `TestConventions_*` subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add docs/cmd/setup-mcp-install.md docs/cmd/setup-mcp-uninstall.md README.md
git commit -m "docs(setup/mcp): add doc pages and README links for install and uninstall"
```

---

## Task 5: Commit the uncommitted internal/endpoint changes

The `internal/endpoint/` package (`identity.go`, `identity_test.go`) and updated `go.mod`/`go.sum` are currently unstaged from a prior session. Commit them now so the branch is clean.

- [ ] **Step 1: Stage and commit**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
git add internal/endpoint/identity.go internal/endpoint/identity_test.go go.mod go.sum
git commit -m "feat(endpoint): add IdentityHeaderValue and BuildMCPConfig helpers"
```

---

## Task 6: Final verification

- [ ] **Step 1: Run the full test suite**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
make test 2>&1 | tail -20
```

Expected: all packages pass. `internal/cmd/setup/mcp` appears in the list.

- [ ] **Step 2: Run lint and convention checks**

```bash
make lint && make lint-conventions 2>&1 | tail -10
```

Expected: 0 issues.

- [ ] **Step 3: Build and smoke-test the binary**

```bash
make build
./bin/safedep setup mcp --help
./bin/safedep setup mcp install --help
./bin/safedep setup mcp uninstall --help
```

Expected: help text renders for all three commands with correct flags and descriptions.
