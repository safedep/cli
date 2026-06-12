package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenCode(t *testing.T) {
	t.Run("Detected is false when config dir absent", func(t *testing.T) {
		o := newOpenCode(t.TempDir())
		assert.False(t, o.Detected())
	})

	t.Run("Detected is true when config dir exists", func(t *testing.T) {
		home := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o700))
		o := newOpenCode(home)
		assert.True(t, o.Detected())
	})

	t.Run("GlobalConfigPath", func(t *testing.T) {
		o := newOpenCode("/home/user")
		assert.Equal(t, "/home/user/.config/opencode/opencode.json", o.GlobalConfigPath())
	})

	t.Run("InjectGlobal writes remote mcp config", func(t *testing.T) {
		o := newOpenCode(t.TempDir())
		require.NoError(t, o.InjectGlobal(testCfg))

		data := readJSONAt(t, o.GlobalConfigPath())
		mcp := data["mcp"].(map[string]any)
		entry := mcp["safedep"].(map[string]any)
		assert.Equal(t, "remote", entry["type"])
		assert.Equal(t, testCfg.URL, entry["url"])
		assert.Equal(t, true, entry["enabled"])
		assert.Equal(t, "Bearer tok", entry["headers"].(map[string]any)["Authorization"])
	})

	t.Run("WorkspaceConfigPath", func(t *testing.T) {
		o := newOpenCode("/home/user")
		assert.Equal(t, "/proj/opencode.json", o.WorkspaceConfigPath("/proj"))
	})

	t.Run("InjectWorkspace writes to project config", func(t *testing.T) {
		o := newOpenCode(t.TempDir())
		workspace := t.TempDir()
		require.NoError(t, o.InjectWorkspace(workspace, testCfg))
		data := readJSONAt(t, o.WorkspaceConfigPath(workspace))
		mcp := data["mcp"].(map[string]any)
		assert.Contains(t, mcp, "safedep")
	})

	t.Run("RemoveWorkspace removes safedep entry", func(t *testing.T) {
		o := newOpenCode(t.TempDir())
		workspace := t.TempDir()
		require.NoError(t, o.InjectWorkspace(workspace, testCfg))
		require.NoError(t, o.RemoveWorkspace(workspace))
		data := readJSONAt(t, o.WorkspaceConfigPath(workspace))
		mcp := data["mcp"].(map[string]any)
		assert.NotContains(t, mcp, "safedep")
	})

	t.Run("GlobalConfigured reads the mcp key", func(t *testing.T) {
		o := newOpenCode(t.TempDir())

		got, err := o.GlobalConfigured()
		require.NoError(t, err)
		assert.False(t, got)

		require.NoError(t, o.InjectGlobal(testCfg))
		got, err = o.GlobalConfigured()
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("WorkspaceConfigured reads the mcp key", func(t *testing.T) {
		o := newOpenCode(t.TempDir())
		workspace := t.TempDir()
		require.NoError(t, o.InjectWorkspace(workspace, testCfg))
		got, err := o.WorkspaceConfigured(workspace)
		require.NoError(t, err)
		assert.True(t, got)
	})
}
