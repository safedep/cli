package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCode(t *testing.T) {
	cfg := MCPConfig{
		URL:     "https://mcp.safedep.io/model-context-protocol/threats/v1/mcp",
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
		assert.Equal(t, "/home/user/.claude.json", cc.GlobalConfigPath())
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

	t.Run("InjectGlobal creates and populates config with type:http", func(t *testing.T) {
		home := t.TempDir()
		cc := newClaudeCode(home)

		require.NoError(t, cc.InjectGlobal(cfg))

		data := readJSONAt(t, cc.GlobalConfigPath())
		servers := data["mcpServers"].(map[string]any)
		entry := servers["safedep"].(map[string]any)
		assert.Equal(t, cfg.URL, entry["url"])
		assert.Equal(t, "http", entry["type"])
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

	t.Run("WorkspaceConfigPath returns ~/.claude.json", func(t *testing.T) {
		cc := newClaudeCode("/home/user")
		assert.Equal(t, "/home/user/.claude.json", cc.WorkspaceConfigPath("/proj"))
	})

	t.Run("InjectWorkspace writes to ~/.claude.json under projects", func(t *testing.T) {
		home := t.TempDir()
		workspace := t.TempDir()
		cc := newClaudeCode(home)

		require.NoError(t, cc.InjectWorkspace(workspace, cfg))

		data := readJSONAt(t, cc.GlobalConfigPath())
		projects := data["projects"].(map[string]any)
		project := projects[workspace].(map[string]any)
		servers := project["mcpServers"].(map[string]any)
		entry := servers["safedep"].(map[string]any)
		assert.Equal(t, cfg.URL, entry["url"])
		assert.Equal(t, "http", entry["type"])
	})

	t.Run("InjectWorkspace preserves existing project keys", func(t *testing.T) {
		home := t.TempDir()
		workspace := t.TempDir()
		cc := newClaudeCode(home)

		// Pre-populate ~/.claude.json with an existing project entry.
		existing := map[string]any{
			"projects": map[string]any{
				workspace: map[string]any{
					"allowedTools": []any{"Bash"},
				},
			},
		}
		encoded, err := json.Marshal(existing)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(cc.GlobalConfigPath(), encoded, 0o600))

		require.NoError(t, cc.InjectWorkspace(workspace, cfg))

		data := readJSONAt(t, cc.GlobalConfigPath())
		project := data["projects"].(map[string]any)[workspace].(map[string]any)
		assert.Contains(t, project, "allowedTools")
		assert.Contains(t, project["mcpServers"].(map[string]any), "safedep")
	})

	t.Run("RemoveWorkspace removes safedep entry", func(t *testing.T) {
		home := t.TempDir()
		workspace := t.TempDir()
		cc := newClaudeCode(home)
		require.NoError(t, cc.InjectWorkspace(workspace, cfg))
		require.NoError(t, cc.RemoveWorkspace(workspace))

		data := readJSONAt(t, cc.GlobalConfigPath())
		projects, _ := data["projects"].(map[string]any)
		if projects != nil {
			if project, ok := projects[workspace].(map[string]any); ok {
				servers, _ := project["mcpServers"].(map[string]any)
				assert.NotContains(t, servers, "safedep")
			}
		}
	})

	t.Run("GlobalConfigured tracks inject and remove", func(t *testing.T) {
		cc := newClaudeCode(t.TempDir())

		got, err := cc.GlobalConfigured()
		require.NoError(t, err)
		assert.False(t, got)

		require.NoError(t, cc.InjectGlobal(cfg))
		got, err = cc.GlobalConfigured()
		require.NoError(t, err)
		assert.True(t, got)

		require.NoError(t, cc.RemoveGlobal())
		got, err = cc.GlobalConfigured()
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("WorkspaceConfigured reads nested projects entry", func(t *testing.T) {
		home := t.TempDir()
		workspace := t.TempDir()
		cc := newClaudeCode(home)

		got, err := cc.WorkspaceConfigured(workspace)
		require.NoError(t, err)
		assert.False(t, got)

		require.NoError(t, cc.InjectWorkspace(workspace, cfg))
		got, err = cc.WorkspaceConfigured(workspace)
		require.NoError(t, err)
		assert.True(t, got)

		// A different project path must not report as configured.
		got, err = cc.WorkspaceConfigured(t.TempDir())
		require.NoError(t, err)
		assert.False(t, got)
	})
}
