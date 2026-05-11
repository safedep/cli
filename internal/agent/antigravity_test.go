package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAntigravity(t *testing.T) {
	t.Run("Detected is false when config dir absent", func(t *testing.T) {
		a := newAntigravity(t.TempDir())
		assert.False(t, a.Detected())
	})

	t.Run("Detected is true when config dir exists", func(t *testing.T) {
		home := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".gemini", "antigravity"), 0o700))
		a := newAntigravity(home)
		assert.True(t, a.Detected())
	})

	t.Run("GlobalConfigPath", func(t *testing.T) {
		a := newAntigravity("/home/user")
		assert.Equal(t, "/home/user/.gemini/antigravity/mcp_config.json", a.GlobalConfigPath())
	})

	t.Run("InjectGlobal writes serverUrl config", func(t *testing.T) {
		a := newAntigravity(t.TempDir())
		require.NoError(t, a.InjectGlobal(testCfg))

		data := readJSONAt(t, a.GlobalConfigPath())
		servers := data["mcpServers"].(map[string]any)
		entry := servers["safedep"].(map[string]any)
		assert.Equal(t, testCfg.URL, entry["serverUrl"])
		assert.Equal(t, "Bearer tok", entry["headers"].(map[string]any)["Authorization"])
		assert.NotContains(t, entry, "url")
	})

	t.Run("RemoveGlobal removes safedep entry", func(t *testing.T) {
		a := newAntigravity(t.TempDir())
		require.NoError(t, a.InjectGlobal(testCfg))
		require.NoError(t, a.RemoveGlobal())
		data := readJSONAt(t, a.GlobalConfigPath())
		servers := data["mcpServers"].(map[string]any)
		assert.NotContains(t, servers, "safedep")
	})

	t.Run("workspace injection is unsupported", func(t *testing.T) {
		a := newAntigravity(t.TempDir())
		_, ok := a.AsWorkspaceInjector()
		assert.False(t, ok)
	})
}
