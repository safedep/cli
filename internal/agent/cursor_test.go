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
		URL:     "https://mcp.safedep.io/model-context-protocol/threats/v1/sse",
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
