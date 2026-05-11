package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiCLI(t *testing.T) {
	t.Run("Detected is false when .gemini dir absent", func(t *testing.T) {
		g := newGeminiCLI(t.TempDir())
		assert.False(t, g.Detected())
	})

	t.Run("Detected is true when .gemini dir exists", func(t *testing.T) {
		home := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".gemini"), 0o700))
		g := newGeminiCLI(home)
		assert.True(t, g.Detected())
	})

	t.Run("GlobalConfigPath", func(t *testing.T) {
		g := newGeminiCLI("/home/user")
		assert.Equal(t, "/home/user/.gemini/settings.json", g.GlobalConfigPath())
	})

	t.Run("InjectGlobal writes to user settings", func(t *testing.T) {
		g := newGeminiCLI(t.TempDir())
		require.NoError(t, g.InjectGlobal(testCfg))
		data := readJSONAt(t, g.GlobalConfigPath())
		servers := data["mcpServers"].(map[string]any)
		assert.Contains(t, servers, "safedep")
	})

	t.Run("WorkspaceConfigPath", func(t *testing.T) {
		g := newGeminiCLI("/home/user")
		assert.Equal(t, "/proj/.gemini/settings.json", g.WorkspaceConfigPath("/proj"))
	})

	t.Run("InjectWorkspace writes to workspace settings", func(t *testing.T) {
		g := newGeminiCLI(t.TempDir())
		workspace := t.TempDir()
		require.NoError(t, g.InjectWorkspace(workspace, testCfg))
		data := readJSONAt(t, g.WorkspaceConfigPath(workspace))
		servers := data["mcpServers"].(map[string]any)
		assert.Contains(t, servers, "safedep")
	})

	t.Run("RemoveWorkspace removes safedep entry", func(t *testing.T) {
		g := newGeminiCLI(t.TempDir())
		workspace := t.TempDir()
		require.NoError(t, g.InjectWorkspace(workspace, testCfg))
		require.NoError(t, g.RemoveWorkspace(workspace))
		data := readJSONAt(t, g.WorkspaceConfigPath(workspace))
		servers := data["mcpServers"].(map[string]any)
		assert.NotContains(t, servers, "safedep")
	})
}
