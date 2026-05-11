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
