# Agent Discovery and MCP Configuration — Design

**Issue:** [control-tower#788](https://github.com/safedep/control-tower/issues/788)
**Date:** 2026-05-11
**Status:** Draft

## Problem

The SafeDep CLI needs a reusable internal package that:

1. Discovers which AI agents are installed on the current machine.
2. Adds or removes the SafeDep MCP server configuration from each agent's config file.

This package is not a command itself. It is the foundation that the future `safedep setup mcp install` and `safedep setup mcp uninstall` commands (Chunk C, issue #789) will call into.

## Scope

In scope:

- `internal/agent/` package with Agent, GlobalInjector, and WorkspaceInjector interfaces.
- Five agent adapters: Claude Code, Cursor, Gemini CLI, OpenCode, Antigravity.
- Idempotent JSON config injection and removal (touches only the `safedep` key under `mcpServers`).
- `InjectAll` and `RemoveAll` orchestration functions.
- Unit tests per adapter using temp filesystem fixtures.

Out of scope:

- The `safedep setup mcp` command and its cobra wiring (Chunk C).
- Onboarding / zero-tenant path (Chunk B, issue #787).
- npm packaging (Chunk D, issue #978).
- Endpoint registration or cloud sync.

## Interface Design

### Why this shape

**The fundamental constraint.** Go has no sealed interfaces or exhaustive dispatch. No design can simultaneously satisfy both: (a) callers do not need updating when a new injection scope is added, and (b) the compiler enforces that every adapter handles the new scope. One side of that boundary always pays. The choice is where.

**Alternatives ruled out.**

*Scope as a method parameter* — a single `InjectMCPConfig(cfg, scope, workspaceDir)` on one interface. Adding a new scope value means every adapter's switch statement needs a new case; the compiler cannot detect a missing case. Ruled out: silent runtime miss, OCP violation inside every adapter.

*Runtime type assertions in the caller* — `if g, ok := a.(GlobalInjector); ok { ... }`. Adding a new scope requires a new `if` block in the caller; forgetting it compiles cleanly and silently skips injection for that scope. Ruled out: same silent-miss problem at the call site.

*Typed slices per scope* — `var globalInjectors []GlobalInjector`. No type assertions in the caller, but registering a new adapter in the right slices is manual; the compiler only catches interface satisfaction, not omission from a slice. A new adapter left out of `workspaceInjectors` silently skips workspace injection. Ruled out: silent-miss moves from the adapter to the registration site.

*Embedding `Agent` inside `GlobalInjector`/`WorkspaceInjector`* — would allow a single slice to carry both detection and injection. Ruled out: violates separation of concerns. An injector's contract is writing config, not knowing whether the agent is present.

**Why capability-query methods win.** `AsGlobalInjector() (GlobalInjector, bool)` on the `Agent` interface means adding a new scope (`AsProjectInjector`) extends the interface. Every existing adapter immediately fails to compile, forcing an explicit opt-out (`return nil, false`). The caller still gains one new `if inj, ok := a.AsProjectInjector(); ok` block when the scope is added — that is unavoidable in Go — but the adapter side is fully compiler-enforced. The `(nil, false)` returns are not noise: they state in code that a given agent has no support for a given scope.

### Types

```go
// MCPConfig is the SafeDep MCP server entry to inject.
type MCPConfig struct {
    URL     string
    Headers map[string]string
}

// Agent is an AI coding agent that may be installed on the current machine.
type Agent interface {
    // Name returns a stable identifier for the agent (e.g. "claude-code").
    Name() string

    // Detected reports whether the agent is installed on this machine.
    // Implementations check for config directory presence or binary on PATH.
    Detected() bool

    // AsGlobalInjector returns the agent's global-scope injector if supported.
    AsGlobalInjector() (GlobalInjector, bool)

    // AsWorkspaceInjector returns the agent's workspace-scope injector if supported.
    AsWorkspaceInjector() (WorkspaceInjector, bool)
}

// GlobalInjector writes or removes the SafeDep MCP config from a user-level
// (home-directory) config file.
type GlobalInjector interface {
    // GlobalConfigPath returns the absolute path to the config file.
    // Used by the command layer for display and logging.
    GlobalConfigPath() string

    // InjectGlobal writes the SafeDep MCP server entry into the global config.
    // Idempotent: repeated calls overwrite the existing entry; other keys
    // in the file are preserved.
    InjectGlobal(cfg MCPConfig) error

    // RemoveGlobal deletes the SafeDep MCP server entry from the global config.
    // No-op if the file or the entry does not exist.
    RemoveGlobal() error
}

// WorkspaceInjector writes or removes the SafeDep MCP config from a
// workspace (project-directory) config file.
type WorkspaceInjector interface {
    // WorkspaceConfigPath returns the absolute path to the workspace config file
    // relative to the given workspace directory.
    WorkspaceConfigPath(workspaceDir string) string

    // InjectWorkspace writes the SafeDep MCP server entry into the workspace config.
    // Idempotent: same semantics as InjectGlobal.
    InjectWorkspace(workspaceDir string, cfg MCPConfig) error

    // RemoveWorkspace deletes the SafeDep MCP server entry from the workspace config.
    // No-op if the file or the entry does not exist.
    RemoveWorkspace(workspaceDir string) error
}
```

## Agent Adapters

Five adapters are defined in this chunk. Config paths for Gemini CLI and OpenCode were verified against upstream documentation before implementation. Antigravity's public setup references consistently document a global MCP config path, but no Google-maintained reference was found, so the adapter supports global config only.

| Agent | Name constant | Global config path | Workspace config path | Detection method |
|---|---|---|---|---|
| Claude Code | `"claude-code"` | `~/.claude/settings.json` | `.claude/settings.json` | `os.Stat("~/.claude/")` |
| Cursor | `"cursor"` | `~/.cursor/mcp.json` | `.cursor/mcp.json` | `os.Stat("~/.cursor/")` |
| Gemini CLI | `"gemini-cli"` | `~/.gemini/settings.json` | `.gemini/settings.json` | `os.Stat("~/.gemini/")` |
| OpenCode | `"opencode"` | `~/.config/opencode/opencode.json` | `opencode.json` | `os.Stat("~/.config/opencode/")` |
| Antigravity | `"antigravity"` | `~/.gemini/antigravity/mcp_config.json` | Unsupported | `os.Stat("~/.gemini/antigravity/")` |

**Deriving config paths from vet:** The vet codebase (`vet/pkg/aitool/`) contains read-only discovery logic for Claude Code, Cursor, and Windsurf. These are the authoritative references for those agents' config paths. Per ADR, the package is not imported; the paths are transcribed and cited.

**Workspace support for CLIs:** Gemini CLI supports `.gemini/settings.json`. OpenCode supports `opencode.json` at the project root. Antigravity remains global-only until a reliable project-scoped config path is available.

## Config File Format

The SafeDep MCP server entry uses the format shared by Claude Code, Cursor, Gemini CLI, and Windsurf (confirmed from vet/pkg/aitool/mcp_config.go and Gemini CLI docs):

```json
{
  "mcpServers": {
    "safedep": {
      "url": "https://mcp.safedep.io/model-context-protocol/threats/v1",
      "headers": {
        "Authorization": "Bearer <api-key>",
        "X-Tenant-ID": "<tenant-id>",
        "X-Endpoint-ID": "<base64-proto-endpoint-identity>"
      }
    }
  }
}
```

Only the `safedep` key under `mcpServers` is owned by the CLI. All other keys in the config file are preserved verbatim.

OpenCode uses its own documented shape under `mcp.safedep`:

```json
{
  "mcp": {
    "safedep": {
      "type": "remote",
      "url": "https://mcp.safedep.io/model-context-protocol/threats/v1",
      "enabled": true,
      "headers": {
        "Authorization": "Bearer <token>",
        "X-Tenant-ID": "<tenant-id>"
      }
    }
  }
}
```

Antigravity uses `mcpServers.safedep.serverUrl` for remote servers:

```json
{
  "mcpServers": {
    "safedep": {
      "serverUrl": "https://mcp.safedep.io/model-context-protocol/threats/v1",
      "headers": {
        "Authorization": "Bearer <token>",
        "X-Tenant-ID": "<tenant-id>"
      }
    }
  }
}
```

## Injection and Removal Algorithm

Implemented in `internal/agent/config.go`. Both operations take an absolute file path and operate on the `mcpServers` key.

### Injection (`writeMCPConfig`)

1. Read the file. If absent, start from an empty object (`{}`).
2. Unmarshal into `map[string]any` — this preserves unknown fields without a schema.
3. If `mcpServers` is absent or not an object, create it as an empty map.
4. Set `mcpServers["safedep"]` to the serialized form of `MCPConfig`. Overwrites any existing entry.
5. Marshal back to JSON with 2-space indentation and write atomically (write to a temp file in the same directory, then rename).

Atomic write prevents partial writes from corrupting the config file.

### Removal (`removeMCPConfig`)

1. Read the file. If absent, return nil (no-op).
2. Unmarshal into `map[string]any`.
3. Delete `mcpServers["safedep"]`. If `mcpServers` is now empty, leave the empty object — do not remove the parent key, as the file may have been created by the agent itself.
4. Write back with the same atomic rename.

## Public Surface

```go
// NewRegistry returns all known agent adapters, initialised with the
// current user's home directory. The caller is responsible for detecting
// which agents are present via Agent.Detected().
func NewRegistry() []Agent

// InjectAll runs global and optional workspace injection for every detected agent.
// workspaceDir is the project root; pass "" to skip workspace injection.
// All agents are attempted regardless of individual failures. Errors are
// collected and returned as a single joined error.
func InjectAll(agents []Agent, cfg MCPConfig, workspaceDir string) error

// RemoveAll removes the SafeDep MCP entry from every detected agent.
// Same error semantics as InjectAll.
func RemoveAll(agents []Agent, workspaceDir string) error
```

## File Layout

```
internal/agent/
  agent.go          interfaces (Agent, GlobalInjector, WorkspaceInjector) + MCPConfig
  registry.go       NewRegistry()
  inject.go         InjectAll, RemoveAll
  config.go         writeMCPConfig, removeMCPConfig (shared JSON helpers)
  claudecode.go     Claude Code adapter
  cursor.go         Cursor adapter
  geminicli.go      Gemini CLI adapter (stub until config paths verified)
  opencode.go       OpenCode adapter (stub until config paths verified)
  antigravity.go    Antigravity adapter (stub until config paths verified)
  config_test.go
  claudecode_test.go
  cursor_test.go
  geminicli_test.go
  opencode_test.go
  antigravity_test.go
  inject_test.go
```

No `service.go` — the orchestration in `inject.go` is the service.

## Error Handling

- `writeMCPConfig` and `removeMCPConfig` return the first filesystem or JSON error encountered.
- `InjectAll` and `RemoveAll` attempt every agent. Errors are accumulated with `errors.Join`. The caller sees a non-nil error if any agent failed, but successful agents are not rolled back.
- An agent where `Detected()` returns false is silently skipped — not an error.
- A config file that exists but contains invalid JSON is an error (do not overwrite silently).

## Testing

### Per-adapter tests

Each adapter test:

1. Creates a temp directory standing in for `$HOME`.
2. Calls `InjectGlobal` — asserts the file exists and `mcpServers.safedep` matches the input.
3. Calls `InjectGlobal` again with different values — asserts idempotency (only `safedep` changed, other keys preserved).
4. Calls `RemoveGlobal` — asserts `mcpServers.safedep` is absent; file still present.
5. Calls `RemoveGlobal` on an absent file — asserts no error.

Workspace variants follow the same pattern with a temp project directory.

### `config.go` tests

Table-driven tests covering: inject into absent file, inject into existing file with other keys, remove from file, remove from absent file, remove from file where `safedep` key is absent, invalid JSON input.

### `inject.go` tests

`InjectAll` with a mix of detected and undetected fake agents, verifying only detected agents are written to. Error accumulation: one failing agent does not prevent others from running.

### Interface compliance

Compile-time assertions in `agent.go` (blank-identifier assignments) confirm each concrete type satisfies `Agent`:

```go
var (
    _ Agent = (*claudeCode)(nil)
    _ Agent = (*cursor)(nil)
    _ Agent = (*geminiCLI)(nil)
    _ Agent = (*openCode)(nil)
    _ Agent = (*antigravity)(nil)
)
```

## Open Items

| Item | Owner | Blocking |
|---|---|---|
| Gemini CLI global and workspace config paths | Implementer | Done |
| OpenCode global and workspace config paths | Implementer | Done |
| Antigravity project-scoped config path | Implementer | No — global-only adapter implemented |
| Windsurf: include or defer? | Team | No — not in issue #788 scope; defer |
| Codex: uses TOML config (`~/.codex/config.toml`). No Go library preserves TOML comments on round-trip. go-toml v1 Tree API (`SetPath`/`DeletePath`/`ToTomlString`) is the viable path when this is revisited. | Implementer | No — deferred |
