# Agent Discovery and MCP Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `internal/agent/` — a reusable package that discovers installed AI agents and injects or removes the SafeDep MCP server config from their config files.

**Architecture:** Five adapters (Claude Code, Cursor, Gemini CLI, OpenCode, Antigravity) each implement a single `Agent` interface with capability-query methods (`AsGlobalInjector`, `AsWorkspaceInjector`). Shared JSON helpers in `config.go` perform atomic, idempotent reads and writes. Claude Code, Cursor, and Gemini CLI use `mcpServers.safedep`. OpenCode uses `mcp.safedep`. Antigravity uses `mcpServers.safedep.serverUrl` and global config only. `InjectAll`/`RemoveAll` in `inject.go` are the only public orchestration surface; command-layer code (Chunk C) calls these.

**Tech Stack:** Go 1.26, `encoding/json`, `os`, `path/filepath`, `errors.Join`, `github.com/safedep/dry/log`, `testify/require` + `testify/assert`.

---

## File Map

| File | Responsibility |
|---|---|
| `internal/agent/agent.go` | Interfaces (`Agent`, `GlobalInjector`, `WorkspaceInjector`), `MCPConfig`, compile-time assertions |
| `internal/agent/config.go` | `writeMCPConfig`, `removeMCPConfig`, atomic JSON helpers |
| `internal/agent/inject.go` | `InjectAll`, `RemoveAll` |
| `internal/agent/registry.go` | `NewRegistry` |
| `internal/agent/claudecode.go` | Claude Code adapter |
| `internal/agent/cursor.go` | Cursor adapter |
| `internal/agent/geminicli.go` | Gemini CLI adapter |
| `internal/agent/opencode.go` | OpenCode adapter |
| `internal/agent/antigravity.go` | Antigravity global adapter |
| `internal/agent/config_test.go` | Tests for JSON helpers + shared `readJSONAt` helper |
| `internal/agent/inject_test.go` | Tests for `InjectAll`/`RemoveAll` with fake agents |
| `internal/agent/claudecode_test.go` | Tests for Claude Code adapter |
| `internal/agent/cursor_test.go` | Tests for Cursor adapter |
| `internal/agent/geminicli_test.go` | Tests for Gemini CLI adapter |
| `internal/agent/opencode_test.go` | Tests for OpenCode adapter |
| `internal/agent/antigravity_test.go` | Tests for Antigravity global adapter |
| `internal/agent/registry_test.go` | Tests for `NewRegistry` |

---

## Task 1: Interfaces and types

**Files:**
- Create: `internal/agent/agent.go`

- [ ] **Step 1: Write `agent.go`**

```go
package agent

// MCPConfig is the SafeDep MCP server entry to inject.
type MCPConfig struct {
	URL     string
	Headers map[string]string
}

// Agent is an AI coding agent that may be installed on the current machine.
type Agent interface {
	// Name returns a stable identifier (e.g. "claude-code").
	Name() string

	// Detected reports whether the agent is installed on this machine.
	Detected() bool

	// AsGlobalInjector returns the user-level injector if this agent supports global config.
	AsGlobalInjector() (GlobalInjector, bool)

	// AsWorkspaceInjector returns the workspace injector if this agent supports project config.
	AsWorkspaceInjector() (WorkspaceInjector, bool)
}

// GlobalInjector writes or removes the SafeDep MCP config from a user-level config file.
type GlobalInjector interface {
	// GlobalConfigPath returns the absolute path to the global config file.
	GlobalConfigPath() string

	// InjectGlobal writes the SafeDep entry. Idempotent; preserves all other keys.
	InjectGlobal(cfg MCPConfig) error

	// RemoveGlobal deletes the SafeDep entry. No-op if absent.
	RemoveGlobal() error
}

// WorkspaceInjector writes or removes the SafeDep MCP config from a workspace config file.
type WorkspaceInjector interface {
	// WorkspaceConfigPath returns the absolute path inside workspaceDir.
	WorkspaceConfigPath(workspaceDir string) string

	// InjectWorkspace writes the SafeDep entry. Idempotent; preserves all other keys.
	InjectWorkspace(workspaceDir string, cfg MCPConfig) error

	// RemoveWorkspace deletes the SafeDep entry. No-op if absent.
	RemoveWorkspace(workspaceDir string) error
}

```

Each adapter file carries its own compile-time assertion (see Tasks 4–6). This keeps each task self-contained: the package compiles after every task without needing all adapters to exist first.

- [ ] **Step 2: Verify `agent.go` compiles on its own**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go vet ./internal/agent/... 2>&1 | grep -v "no Go files"
```

Expected: no output (interfaces have no bodies to check at this stage).

---

## Task 2: JSON config helpers

**Files:**
- Create: `internal/agent/config.go`
- Create: `internal/agent/config_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/agent/config_test.go
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testCfg = MCPConfig{
	URL: "https://mcp.safedep.io/model-context-protocol/threats/v1",
	Headers: map[string]string{
		"Authorization": "Bearer tok",
		"X-Tenant-ID":   "tenant-1",
	},
}

func TestWriteMCPConfig(t *testing.T) {
	t.Run("creates file when absent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")

		require.NoError(t, writeMCPConfig(path, testCfg))

		data := readJSONAt(t, path)
		servers := data["mcpServers"].(map[string]any)
		entry := servers["safedep"].(map[string]any)
		assert.Equal(t, testCfg.URL, entry["url"])
	})

	t.Run("preserves other top-level keys and other mcpServers entries", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		existing := `{"model":"claude-3","mcpServers":{"other":{"url":"http://other"}}}`
		require.NoError(t, os.WriteFile(path, []byte(existing), 0o600))

		require.NoError(t, writeMCPConfig(path, testCfg))

		data := readJSONAt(t, path)
		assert.Equal(t, "claude-3", data["model"])
		servers := data["mcpServers"].(map[string]any)
		assert.Contains(t, servers, "other")
		assert.Contains(t, servers, "safedep")
	})

	t.Run("idempotent: overwrites existing safedep entry", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		require.NoError(t, writeMCPConfig(path, testCfg))

		cfg2 := MCPConfig{URL: "https://other-url", Headers: map[string]string{"X-Tenant-ID": "t2"}}
		require.NoError(t, writeMCPConfig(path, cfg2))

		data := readJSONAt(t, path)
		servers := data["mcpServers"].(map[string]any)
		entry := servers["safedep"].(map[string]any)
		assert.Equal(t, "https://other-url", entry["url"])
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

		require.Error(t, writeMCPConfig(path, testCfg))
	})
}

func TestRemoveMCPConfig(t *testing.T) {
	t.Run("no-op on absent file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		require.NoError(t, removeMCPConfig(path))
	})

	t.Run("removes safedep key", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		require.NoError(t, writeMCPConfig(path, testCfg))

		require.NoError(t, removeMCPConfig(path))

		data := readJSONAt(t, path)
		servers, _ := data["mcpServers"].(map[string]any)
		assert.NotContains(t, servers, "safedep")
	})

	t.Run("preserves other mcpServers entries after removal", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		existing := `{"mcpServers":{"other":{"url":"http://other"},"safedep":{"url":"http://sd"}}}`
		require.NoError(t, os.WriteFile(path, []byte(existing), 0o600))

		require.NoError(t, removeMCPConfig(path))

		data := readJSONAt(t, path)
		servers := data["mcpServers"].(map[string]any)
		assert.Contains(t, servers, "other")
		assert.NotContains(t, servers, "safedep")
	})

	t.Run("no-op when safedep key is already absent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"mcpServers":{"other":{"url":"http://x"}}}`), 0o600))
		require.NoError(t, removeMCPConfig(path))
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))
		require.Error(t, removeMCPConfig(path))
	})
}

// readJSONAt is a test helper used across all adapter tests in this package.
func readJSONAt(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(raw, &data))
	return data
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... 2>&1 | head -20
```

Expected: compilation errors (functions undefined).

- [ ] **Step 3: Write `config.go`**

```go
// internal/agent/config.go
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const safedepMCPKey = "safedep"

// mcpServerEntry is the JSON shape written under mcpServers.safedep.
type mcpServerEntry struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// writeMCPConfig writes the SafeDep MCP server entry into the JSON config file at path.
// Creates the file if absent. Preserves all other keys. Write is atomic.
func writeMCPConfig(path string, cfg MCPConfig) error {
	data, err := readJSONFile(path)
	if err != nil {
		return err
	}

	servers, err := ensureMCPServers(data)
	if err != nil {
		return err
	}

	servers[safedepMCPKey] = mcpServerEntry{URL: cfg.URL, Headers: cfg.Headers}
	data["mcpServers"] = servers

	return writeJSONFile(path, data)
}

// removeMCPConfig deletes the SafeDep MCP server entry from the config file at path.
// No-op if the file or the entry does not exist.
func removeMCPConfig(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("agent: parse %s: %w", path, err)
	}

	servers, ok := data["mcpServers"].(map[string]any)
	if !ok {
		return nil
	}

	delete(servers, safedepMCPKey)
	data["mcpServers"] = servers

	return writeJSONFile(path, data)
}

// readJSONFile reads and unmarshals a JSON file. Returns an empty map when
// the file does not exist so the caller can create one from scratch.
func readJSONFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("agent: parse %s: %w", path, err)
	}

	return data, nil
}

// ensureMCPServers returns the mcpServers sub-map from data, creating it if
// absent. Returns an error when the existing value is not a JSON object.
func ensureMCPServers(data map[string]any) (map[string]any, error) {
	v, ok := data["mcpServers"]
	if !ok || v == nil {
		return make(map[string]any), nil
	}

	servers, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("agent: mcpServers is not an object")
	}

	return servers, nil
}

// writeJSONFile writes data as 2-space-indented JSON to path atomically by
// writing a temp file in the same directory and renaming it over the target.
func writeJSONFile(path string, data map[string]any) error {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".safedep-mcp-*.json")
	if err != nil {
		return err
	}

	tmpName := tmp.Name()

	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	return os.Rename(tmpName, path)
}
```

- [ ] **Step 4: Run the config tests**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run TestWriteMCPConfig -v
go test ./internal/agent/... -run TestRemoveMCPConfig -v
```

Expected: all pass. If any fail, fix before continuing.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/config.go internal/agent/config_test.go
git commit -m "feat(agent): add Agent interfaces and JSON config helpers"
```

---

## Task 3: InjectAll and RemoveAll

**Files:**
- Create: `internal/agent/inject.go`
- Create: `internal/agent/inject_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/agent/inject_test.go
package agent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes ---

type fakeGlobalInjector struct {
	path      string
	injected  *MCPConfig
	removed   bool
	injectErr error
	removeErr error
}

func (f *fakeGlobalInjector) GlobalConfigPath() string { return f.path }
func (f *fakeGlobalInjector) InjectGlobal(cfg MCPConfig) error {
	if f.injectErr != nil {
		return f.injectErr
	}
	c := cfg
	f.injected = &c
	return nil
}
func (f *fakeGlobalInjector) RemoveGlobal() error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = true
	return nil
}

type fakeWorkspaceInjector struct {
	path      string
	injected  *MCPConfig
	removed   bool
	injectErr error
	removeErr error
}

func (f *fakeWorkspaceInjector) WorkspaceConfigPath(_ string) string { return f.path }
func (f *fakeWorkspaceInjector) InjectWorkspace(_ string, cfg MCPConfig) error {
	if f.injectErr != nil {
		return f.injectErr
	}
	c := cfg
	f.injected = &c
	return nil
}
func (f *fakeWorkspaceInjector) RemoveWorkspace(_ string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = true
	return nil
}

type fakeAgent struct {
	name     string
	detected bool
	global   *fakeGlobalInjector
	workspace *fakeWorkspaceInjector
}

func (f *fakeAgent) Name() string     { return f.name }
func (f *fakeAgent) Detected() bool   { return f.detected }
func (f *fakeAgent) AsGlobalInjector() (GlobalInjector, bool) {
	if f.global == nil {
		return nil, false
	}
	return f.global, true
}
func (f *fakeAgent) AsWorkspaceInjector() (WorkspaceInjector, bool) {
	if f.workspace == nil {
		return nil, false
	}
	return f.workspace, true
}

// --- tests ---

func TestInjectAll(t *testing.T) {
	cfg := MCPConfig{URL: "https://mcp.safedep.io"}

	t.Run("skips undetected agents", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "x", detected: false, global: gi}

		require.NoError(t, InjectAll([]Agent{a}, cfg, ""))
		assert.Nil(t, gi.injected)
	})

	t.Run("injects global for detected agent", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "x", detected: true, global: gi}

		require.NoError(t, InjectAll([]Agent{a}, cfg, ""))
		require.NotNil(t, gi.injected)
		assert.Equal(t, cfg.URL, gi.injected.URL)
	})

	t.Run("skips workspace when workspaceDir is empty", func(t *testing.T) {
		wi := &fakeWorkspaceInjector{}
		a := &fakeAgent{name: "x", detected: true, workspace: wi}

		require.NoError(t, InjectAll([]Agent{a}, cfg, ""))
		assert.Nil(t, wi.injected)
	})

	t.Run("injects workspace when workspaceDir is set", func(t *testing.T) {
		wi := &fakeWorkspaceInjector{}
		a := &fakeAgent{name: "x", detected: true, workspace: wi}

		require.NoError(t, InjectAll([]Agent{a}, cfg, "/project"))
		require.NotNil(t, wi.injected)
	})

	t.Run("accumulates errors and continues across agents", func(t *testing.T) {
		gi1 := &fakeGlobalInjector{injectErr: errors.New("agent-1-fail")}
		gi2 := &fakeGlobalInjector{}
		a1 := &fakeAgent{name: "a1", detected: true, global: gi1}
		a2 := &fakeAgent{name: "a2", detected: true, global: gi2}

		err := InjectAll([]Agent{a1, a2}, cfg, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agent-1-fail")
		assert.NotNil(t, gi2.injected, "a2 must still be attempted after a1 fails")
	})

	t.Run("agent with no global injector is silently skipped", func(t *testing.T) {
		a := &fakeAgent{name: "x", detected: true, global: nil}
		require.NoError(t, InjectAll([]Agent{a}, cfg, ""))
	})
}

func TestRemoveAll(t *testing.T) {
	t.Run("removes from detected agent", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "x", detected: true, global: gi}

		require.NoError(t, RemoveAll([]Agent{a}, ""))
		assert.True(t, gi.removed)
	})

	t.Run("skips undetected agent", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "x", detected: false, global: gi}

		require.NoError(t, RemoveAll([]Agent{a}, ""))
		assert.False(t, gi.removed)
	})

	t.Run("accumulates errors and continues", func(t *testing.T) {
		gi1 := &fakeGlobalInjector{removeErr: errors.New("remove-fail")}
		gi2 := &fakeGlobalInjector{}
		a1 := &fakeAgent{name: "a1", detected: true, global: gi1}
		a2 := &fakeAgent{name: "a2", detected: true, global: gi2}

		err := RemoveAll([]Agent{a1, a2}, "")
		require.Error(t, err)
		assert.True(t, gi2.removed, "a2 must still be attempted")
	})

	t.Run("removes workspace when workspaceDir is set", func(t *testing.T) {
		wi := &fakeWorkspaceInjector{}
		a := &fakeAgent{name: "x", detected: true, workspace: wi}

		require.NoError(t, RemoveAll([]Agent{a}, "/project"))
		assert.True(t, wi.removed)
	})
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run "TestInjectAll|TestRemoveAll" 2>&1 | head -10
```

Expected: compilation error (`InjectAll` undefined).

- [ ] **Step 3: Write `inject.go`**

```go
// internal/agent/inject.go
package agent

import "errors"

// InjectAll injects the SafeDep MCP config into every detected agent.
// workspaceDir="" skips workspace injection. Best-effort: all agents
// are attempted; errors are accumulated.
func InjectAll(agents []Agent, cfg MCPConfig, workspaceDir string) error {
	var errs []error

	for _, a := range agents {
		if !a.Detected() {
			continue
		}

		if inj, ok := a.AsGlobalInjector(); ok {
			if err := inj.InjectGlobal(cfg); err != nil {
				errs = append(errs, err)
			}
		}

		if workspaceDir != "" {
			if inj, ok := a.AsWorkspaceInjector(); ok {
				if err := inj.InjectWorkspace(workspaceDir, cfg); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	return errors.Join(errs...)
}

// RemoveAll removes the SafeDep MCP config from every detected agent.
// Same error semantics as InjectAll.
func RemoveAll(agents []Agent, workspaceDir string) error {
	var errs []error

	for _, a := range agents {
		if !a.Detected() {
			continue
		}

		if inj, ok := a.AsGlobalInjector(); ok {
			if err := inj.RemoveGlobal(); err != nil {
				errs = append(errs, err)
			}
		}

		if workspaceDir != "" {
			if inj, ok := a.AsWorkspaceInjector(); ok {
				if err := inj.RemoveWorkspace(workspaceDir); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	return errors.Join(errs...)
}
```

- [ ] **Step 4: Run inject tests**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run "TestInjectAll|TestRemoveAll" -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/inject.go internal/agent/inject_test.go
git commit -m "feat(agent): add InjectAll and RemoveAll orchestration"
```

---

## Task 4: Stub adapters (Gemini CLI, OpenCode, Antigravity)

These stubs satisfy the `Agent` interface and keep compile-time assertions in `agent.go` happy. They always report `Detected() = false` and return `(nil, false)` for both injectors until config paths are verified and implemented.

**Files:**
- Create: `internal/agent/geminicli.go`
- Create: `internal/agent/opencode.go`
- Create: `internal/agent/antigravity.go`
- Create: `internal/agent/geminicli_test.go`
- Create: `internal/agent/opencode_test.go`
- Create: `internal/agent/antigravity_test.go`

- [ ] **Step 1: Write `geminicli.go`**

```go
// internal/agent/geminicli.go
package agent

// geminiCLI is a stub. Config paths for Gemini CLI are unverified.
// Detected always returns false until the adapter is fully implemented.
type geminiCLI struct {
	homeDir string
}

func newGeminiCLI(homeDir string) *geminiCLI {
	return &geminiCLI{homeDir: homeDir}
}

func (g *geminiCLI) Name() string                                    { return "gemini-cli" }
func (g *geminiCLI) Detected() bool                                  { return false }
func (g *geminiCLI) AsGlobalInjector() (GlobalInjector, bool)        { return nil, false }
func (g *geminiCLI) AsWorkspaceInjector() (WorkspaceInjector, bool)  { return nil, false }

var _ Agent = (*geminiCLI)(nil)
```

- [ ] **Step 2: Write `opencode.go`**

```go
// internal/agent/opencode.go
package agent

// openCode is a stub. Config paths for OpenCode are unverified.
type openCode struct {
	homeDir string
}

func newOpenCode(homeDir string) *openCode {
	return &openCode{homeDir: homeDir}
}

func (o *openCode) Name() string                                    { return "opencode" }
func (o *openCode) Detected() bool                                  { return false }
func (o *openCode) AsGlobalInjector() (GlobalInjector, bool)        { return nil, false }
func (o *openCode) AsWorkspaceInjector() (WorkspaceInjector, bool)  { return nil, false }

var _ Agent = (*openCode)(nil)
```

- [ ] **Step 3: Write `antigravity.go`**

```go
// internal/agent/antigravity.go
package agent

// antigravity is a stub. Config paths for Antigravity are unverified.
type antigravity struct {
	homeDir string
}

func newAntigravity(homeDir string) *antigravity {
	return &antigravity{homeDir: homeDir}
}

func (a *antigravity) Name() string                                    { return "antigravity" }
func (a *antigravity) Detected() bool                                  { return false }
func (a *antigravity) AsGlobalInjector() (GlobalInjector, bool)        { return nil, false }
func (a *antigravity) AsWorkspaceInjector() (WorkspaceInjector, bool)  { return nil, false }

var _ Agent = (*antigravity)(nil)
```

- [ ] **Step 4: Write stub tests**

```go
// internal/agent/geminicli_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeminiCLIStub(t *testing.T) {
	g := newGeminiCLI(t.TempDir())
	assert.Equal(t, "gemini-cli", g.Name())
	assert.False(t, g.Detected())
	_, ok := g.AsGlobalInjector()
	assert.False(t, ok)
	_, ok = g.AsWorkspaceInjector()
	assert.False(t, ok)
}
```

```go
// internal/agent/opencode_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenCodeStub(t *testing.T) {
	o := newOpenCode(t.TempDir())
	assert.Equal(t, "opencode", o.Name())
	assert.False(t, o.Detected())
	_, ok := o.AsGlobalInjector()
	assert.False(t, ok)
	_, ok = o.AsWorkspaceInjector()
	assert.False(t, ok)
}
```

```go
// internal/agent/antigravity_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAntigravityStub(t *testing.T) {
	a := newAntigravity(t.TempDir())
	assert.Equal(t, "antigravity", a.Name())
	assert.False(t, a.Detected())
	_, ok := a.AsGlobalInjector()
	assert.False(t, ok)
	_, ok = a.AsWorkspaceInjector()
	assert.False(t, ok)
}
```

- [ ] **Step 5: Run stub tests**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run "TestGeminiCLIStub|TestOpenCodeStub|TestAntigravityStub" -v
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/geminicli.go internal/agent/opencode.go internal/agent/antigravity.go \
        internal/agent/geminicli_test.go internal/agent/opencode_test.go internal/agent/antigravity_test.go
git commit -m "feat(agent): add stub adapters for Gemini CLI, OpenCode, Antigravity"
```

---

## Task 5: Claude Code adapter

**Files:**
- Create: `internal/agent/claudecode.go`
- Create: `internal/agent/claudecode_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/agent/claudecode_test.go
package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCode(t *testing.T) {
	cfg := MCPConfig{
		URL:     "https://mcp.safedep.io/model-context-protocol/threats/v1",
		Headers: map[string]string{"Authorization": "Bearer tok"},
	}

	t.Run("Detected is false when .claude dir absent", func(t *testing.T) {
		cc := newClaudeCode(t.TempDir())
		assert.False(t, cc.Detected())
	})

	t.Run("Detected is true when .claude dir exists", func(t *testing.T) {
		home := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o700))
		cc := newClaudeCode(home)
		assert.True(t, cc.Detected())
	})

	t.Run("Name", func(t *testing.T) {
		assert.Equal(t, "claude-code", newClaudeCode("").Name())
	})

	t.Run("GlobalConfigPath", func(t *testing.T) {
		cc := newClaudeCode("/home/user")
		assert.Equal(t, "/home/user/.claude/settings.json", cc.GlobalConfigPath())
	})

	t.Run("AsGlobalInjector returns self", func(t *testing.T) {
		cc := newClaudeCode("")
		inj, ok := cc.AsGlobalInjector()
		assert.True(t, ok)
		assert.NotNil(t, inj)
	})

	t.Run("AsWorkspaceInjector returns self", func(t *testing.T) {
		cc := newClaudeCode("")
		inj, ok := cc.AsWorkspaceInjector()
		assert.True(t, ok)
		assert.NotNil(t, inj)
	})

	t.Run("InjectGlobal creates and populates config", func(t *testing.T) {
		home := t.TempDir()
		cc := newClaudeCode(home)

		require.NoError(t, cc.InjectGlobal(cfg))

		data := readJSONAt(t, cc.GlobalConfigPath())
		servers := data["mcpServers"].(map[string]any)
		entry := servers["safedep"].(map[string]any)
		assert.Equal(t, cfg.URL, entry["url"])
	})

	t.Run("InjectGlobal is idempotent", func(t *testing.T) {
		home := t.TempDir()
		cc := newClaudeCode(home)
		require.NoError(t, cc.InjectGlobal(cfg))

		cfg2 := MCPConfig{URL: "https://other", Headers: map[string]string{"X-Tenant-ID": "t2"}}
		require.NoError(t, cc.InjectGlobal(cfg2))

		data := readJSONAt(t, cc.GlobalConfigPath())
		entry := data["mcpServers"].(map[string]any)["safedep"].(map[string]any)
		assert.Equal(t, "https://other", entry["url"])
	})

	t.Run("RemoveGlobal removes safedep entry", func(t *testing.T) {
		home := t.TempDir()
		cc := newClaudeCode(home)
		require.NoError(t, cc.InjectGlobal(cfg))
		require.NoError(t, cc.RemoveGlobal())

		data := readJSONAt(t, cc.GlobalConfigPath())
		servers, _ := data["mcpServers"].(map[string]any)
		assert.NotContains(t, servers, "safedep")
	})

	t.Run("RemoveGlobal is no-op on absent file", func(t *testing.T) {
		cc := newClaudeCode(t.TempDir())
		require.NoError(t, cc.RemoveGlobal())
	})

	t.Run("WorkspaceConfigPath", func(t *testing.T) {
		cc := newClaudeCode("")
		assert.Equal(t, "/proj/.claude/settings.json", cc.WorkspaceConfigPath("/proj"))
	})

	t.Run("InjectWorkspace writes to workspace dir", func(t *testing.T) {
		workspace := t.TempDir()
		cc := newClaudeCode(t.TempDir())

		require.NoError(t, cc.InjectWorkspace(workspace, cfg))

		data := readJSONAt(t, cc.WorkspaceConfigPath(workspace))
		servers := data["mcpServers"].(map[string]any)
		assert.Contains(t, servers, "safedep")
	})

	t.Run("RemoveWorkspace removes safedep entry", func(t *testing.T) {
		workspace := t.TempDir()
		cc := newClaudeCode(t.TempDir())
		require.NoError(t, cc.InjectWorkspace(workspace, cfg))
		require.NoError(t, cc.RemoveWorkspace(workspace))

		data := readJSONAt(t, cc.WorkspaceConfigPath(workspace))
		servers, _ := data["mcpServers"].(map[string]any)
		assert.NotContains(t, servers, "safedep")
	})
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run TestClaudeCode 2>&1 | head -10
```

Expected: compilation error (`newClaudeCode` undefined).

- [ ] **Step 3: Write `claudecode.go`**

```go
// internal/agent/claudecode.go
package agent

import (
	"os"
	"path/filepath"
)

type claudeCode struct {
	homeDir string
}

func newClaudeCode(homeDir string) *claudeCode {
	return &claudeCode{homeDir: homeDir}
}

func (c *claudeCode) Name() string { return "claude-code" }

func (c *claudeCode) Detected() bool {
	_, err := os.Stat(filepath.Join(c.homeDir, ".claude"))
	return err == nil
}

func (c *claudeCode) AsGlobalInjector() (GlobalInjector, bool)       { return c, true }
func (c *claudeCode) AsWorkspaceInjector() (WorkspaceInjector, bool) { return c, true }

func (c *claudeCode) GlobalConfigPath() string {
	return filepath.Join(c.homeDir, ".claude", "settings.json")
}

func (c *claudeCode) InjectGlobal(cfg MCPConfig) error {
	return writeMCPConfig(c.GlobalConfigPath(), cfg)
}

func (c *claudeCode) RemoveGlobal() error {
	return removeMCPConfig(c.GlobalConfigPath())
}

func (c *claudeCode) WorkspaceConfigPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".claude", "settings.json")
}

func (c *claudeCode) InjectWorkspace(workspaceDir string, cfg MCPConfig) error {
	return writeMCPConfig(c.WorkspaceConfigPath(workspaceDir), cfg)
}

func (c *claudeCode) RemoveWorkspace(workspaceDir string) error {
	return removeMCPConfig(c.WorkspaceConfigPath(workspaceDir))
}

var _ Agent = (*claudeCode)(nil)
```

- [ ] **Step 4: Run Claude Code tests**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run TestClaudeCode -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/claudecode.go internal/agent/claudecode_test.go
git commit -m "feat(agent): implement Claude Code adapter"
```

---

## Task 6: Cursor adapter

**Files:**
- Create: `internal/agent/cursor.go`
- Create: `internal/agent/cursor_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/agent/cursor_test.go
package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCursor(t *testing.T) {
	cfg := MCPConfig{
		URL:     "https://mcp.safedep.io/model-context-protocol/threats/v1",
		Headers: map[string]string{"Authorization": "Bearer tok"},
	}

	t.Run("Detected is false when .cursor dir absent", func(t *testing.T) {
		c := newCursor(t.TempDir())
		assert.False(t, c.Detected())
	})

	t.Run("Detected is true when .cursor dir exists", func(t *testing.T) {
		home := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".cursor"), 0o700))
		c := newCursor(home)
		assert.True(t, c.Detected())
	})

	t.Run("Name", func(t *testing.T) {
		assert.Equal(t, "cursor", newCursor("").Name())
	})

	t.Run("GlobalConfigPath", func(t *testing.T) {
		c := newCursor("/home/user")
		assert.Equal(t, "/home/user/.cursor/mcp.json", c.GlobalConfigPath())
	})

	t.Run("AsGlobalInjector returns self", func(t *testing.T) {
		c := newCursor("")
		inj, ok := c.AsGlobalInjector()
		assert.True(t, ok)
		assert.NotNil(t, inj)
	})

	t.Run("AsWorkspaceInjector returns self", func(t *testing.T) {
		c := newCursor("")
		inj, ok := c.AsWorkspaceInjector()
		assert.True(t, ok)
		assert.NotNil(t, inj)
	})

	t.Run("InjectGlobal creates config", func(t *testing.T) {
		home := t.TempDir()
		c := newCursor(home)

		require.NoError(t, c.InjectGlobal(cfg))

		data := readJSONAt(t, c.GlobalConfigPath())
		servers := data["mcpServers"].(map[string]any)
		entry := servers["safedep"].(map[string]any)
		assert.Equal(t, cfg.URL, entry["url"])
	})

	t.Run("RemoveGlobal removes safedep entry", func(t *testing.T) {
		home := t.TempDir()
		c := newCursor(home)
		require.NoError(t, c.InjectGlobal(cfg))
		require.NoError(t, c.RemoveGlobal())

		data := readJSONAt(t, c.GlobalConfigPath())
		servers, _ := data["mcpServers"].(map[string]any)
		assert.NotContains(t, servers, "safedep")
	})

	t.Run("RemoveGlobal is no-op on absent file", func(t *testing.T) {
		require.NoError(t, newCursor(t.TempDir()).RemoveGlobal())
	})

	t.Run("WorkspaceConfigPath", func(t *testing.T) {
		c := newCursor("")
		assert.Equal(t, "/proj/.cursor/mcp.json", c.WorkspaceConfigPath("/proj"))
	})

	t.Run("InjectWorkspace writes to workspace dir", func(t *testing.T) {
		workspace := t.TempDir()
		c := newCursor(t.TempDir())

		require.NoError(t, c.InjectWorkspace(workspace, cfg))

		data := readJSONAt(t, c.WorkspaceConfigPath(workspace))
		assert.Contains(t, data["mcpServers"].(map[string]any), "safedep")
	})

	t.Run("RemoveWorkspace removes safedep entry", func(t *testing.T) {
		workspace := t.TempDir()
		c := newCursor(t.TempDir())
		require.NoError(t, c.InjectWorkspace(workspace, cfg))
		require.NoError(t, c.RemoveWorkspace(workspace))

		data := readJSONAt(t, c.WorkspaceConfigPath(workspace))
		servers, _ := data["mcpServers"].(map[string]any)
		assert.NotContains(t, servers, "safedep")
	})
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run TestCursor 2>&1 | head -10
```

Expected: compilation error (`newCursor` undefined).

- [ ] **Step 3: Write `cursor.go`**

```go
// internal/agent/cursor.go
package agent

import (
	"os"
	"path/filepath"
)

type cursor struct {
	homeDir string
}

func newCursor(homeDir string) *cursor {
	return &cursor{homeDir: homeDir}
}

func (c *cursor) Name() string { return "cursor" }

func (c *cursor) Detected() bool {
	_, err := os.Stat(filepath.Join(c.homeDir, ".cursor"))
	return err == nil
}

func (c *cursor) AsGlobalInjector() (GlobalInjector, bool)       { return c, true }
func (c *cursor) AsWorkspaceInjector() (WorkspaceInjector, bool) { return c, true }

func (c *cursor) GlobalConfigPath() string {
	return filepath.Join(c.homeDir, ".cursor", "mcp.json")
}

func (c *cursor) InjectGlobal(cfg MCPConfig) error {
	return writeMCPConfig(c.GlobalConfigPath(), cfg)
}

func (c *cursor) RemoveGlobal() error {
	return removeMCPConfig(c.GlobalConfigPath())
}

func (c *cursor) WorkspaceConfigPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".cursor", "mcp.json")
}

func (c *cursor) InjectWorkspace(workspaceDir string, cfg MCPConfig) error {
	return writeMCPConfig(c.WorkspaceConfigPath(workspaceDir), cfg)
}

func (c *cursor) RemoveWorkspace(workspaceDir string) error {
	return removeMCPConfig(c.WorkspaceConfigPath(workspaceDir))
}

var _ Agent = (*cursor)(nil)
```

- [ ] **Step 4: Run Cursor tests**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run TestCursor -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/cursor.go internal/agent/cursor_test.go
git commit -m "feat(agent): implement Cursor adapter"
```

---

## Task 7: Registry

**Files:**
- Create: `internal/agent/registry.go`
- Create: `internal/agent/registry_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/agent/registry_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry(t *testing.T) {
	agents := NewRegistry()
	require.NotEmpty(t, agents)

	names := make(map[string]bool, len(agents))
	for _, a := range agents {
		names[a.Name()] = true
	}

	assert.True(t, names["claude-code"], "registry must include claude-code")
	assert.True(t, names["cursor"], "registry must include cursor")
	assert.True(t, names["gemini-cli"], "registry must include gemini-cli")
	assert.True(t, names["opencode"], "registry must include opencode")
	assert.True(t, names["antigravity"], "registry must include antigravity")
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run TestNewRegistry 2>&1 | head -10
```

Expected: compilation error (`NewRegistry` undefined).

- [ ] **Step 3: Write `registry.go`**

```go
// internal/agent/registry.go
package agent

import (
	"os"

	"github.com/safedep/dry/log"
)

// NewRegistry returns all known agent adapters initialised with the
// current user's home directory.
func NewRegistry() []Agent {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Warnf("agent: registry: could not resolve home directory: %v", err)
		homeDir = ""
	}

	return []Agent{
		newClaudeCode(homeDir),
		newCursor(homeDir),
		newGeminiCLI(homeDir),
		newOpenCode(homeDir),
		newAntigravity(homeDir),
	}
}
```

- [ ] **Step 4: Run registry tests**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -run TestNewRegistry -v
```

Expected: passes.

- [ ] **Step 5: Run the full package test suite and lint**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
go test ./internal/agent/... -v
make lint
```

Expected: all tests pass, 0 lint issues.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/registry.go internal/agent/registry_test.go
git commit -m "feat(agent): add NewRegistry"
```

---

## Task 8: Final verification

- [ ] **Step 1: Run the full test suite**

```bash
cd /home/arunb/work-related/safedep-related/safedep-cli
make test
```

Expected: all tests pass, including the full suite outside `internal/agent/`.

- [ ] **Step 2: Run lint**

```bash
make lint
make lint-conventions
```

Expected: 0 issues.

- [ ] **Step 3: Build the binary**

```bash
make build
```

Expected: `bin/safedep` produced without errors.
