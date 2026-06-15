package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVSCode(t *testing.T) {
	t.Run("Detected is false when neither .vscode-server nor .config/Code present", func(t *testing.T) {
		v := newVSCode(t.TempDir())
		assert.False(t, v.Detected())
	})

	t.Run("Detected is true when .vscode-server exists", func(t *testing.T) {
		home := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".vscode-server"), 0o700))
		v := newVSCode(home)
		assert.True(t, v.Detected())
	})

	t.Run("Detected is true when user config dir exists", func(t *testing.T) {
		home := t.TempDir()
		v := newVSCode(home)
		require.NoError(t, os.MkdirAll(v.userConfigDir(), 0o700))
		assert.True(t, v.Detected())
	})

	t.Run("Name", func(t *testing.T) {
		assert.Equal(t, "vscode", newVSCode("").Name())
	})

	t.Run("AsGlobalInjector returns self", func(t *testing.T) {
		inj, ok := newVSCode("").AsGlobalInjector()
		assert.True(t, ok)
		assert.NotNil(t, inj)
	})

	t.Run("GlobalConfigPath is OS-specific", func(t *testing.T) {
		v := newVSCode("/home/user")
		path := v.GlobalConfigPath()
		// Linux (test host): ~/.config/Code/User/mcp.json
		// macOS:             ~/Library/Application Support/Code/User/mcp.json
		// Windows:           ~\AppData\Roaming\Code\User\mcp.json
		assert.Contains(t, path, "Code")
		assert.Contains(t, path, "mcp.json")
	})

	t.Run("InjectGlobal writes servers key with type:http", func(t *testing.T) {
		home := t.TempDir()
		v := newVSCode(home)

		require.NoError(t, v.InjectGlobal(testCfg))

		data := readJSONAt(t, v.GlobalConfigPath())
		servers := data["servers"].(map[string]any)
		entry := servers["safedep"].(map[string]any)
		assert.Equal(t, "http", entry["type"])
		assert.Equal(t, testCfg.URL, entry["url"])
	})

	t.Run("RemoveGlobal removes safedep entry", func(t *testing.T) {
		home := t.TempDir()
		v := newVSCode(home)
		require.NoError(t, v.InjectGlobal(testCfg))
		require.NoError(t, v.RemoveGlobal())

		data := readJSONAt(t, v.GlobalConfigPath())
		servers, _ := data["servers"].(map[string]any)
		assert.NotContains(t, servers, "safedep")
	})

	t.Run("RemoveGlobal is no-op on absent file", func(t *testing.T) {
		require.NoError(t, newVSCode(t.TempDir()).RemoveGlobal())
	})

	t.Run("AsWorkspaceInjector returns self", func(t *testing.T) {
		inj, ok := newVSCode("").AsWorkspaceInjector()
		assert.True(t, ok)
		assert.NotNil(t, inj)
	})

	t.Run("WorkspaceConfigPath", func(t *testing.T) {
		v := newVSCode("")
		assert.Equal(t, "/proj/.vscode/mcp.json", v.WorkspaceConfigPath("/proj"))
	})

	t.Run("InjectWorkspace writes servers key with type:http", func(t *testing.T) {
		workspace := t.TempDir()
		v := newVSCode(t.TempDir())

		require.NoError(t, v.InjectWorkspace(workspace, testCfg))

		data := readJSONAt(t, v.WorkspaceConfigPath(workspace))
		servers := data["servers"].(map[string]any)
		entry := servers["safedep"].(map[string]any)
		assert.Equal(t, "http", entry["type"])
		assert.Equal(t, testCfg.URL, entry["url"])
		assert.Equal(t, "Bearer tok", entry["headers"].(map[string]any)["Authorization"])
		assert.NotContains(t, data, "mcpServers")
	})

	t.Run("InjectWorkspace is idempotent and preserves other servers", func(t *testing.T) {
		workspace := t.TempDir()
		v := newVSCode(t.TempDir())

		// Pre-existing server — create the directory before writing the file.
		existing := `{"servers":{"github":{"url":"https://api.githubcopilot.com/mcp/"}}}`
		require.NoError(t, os.MkdirAll(filepath.Dir(v.WorkspaceConfigPath(workspace)), 0o700))
		require.NoError(t, os.WriteFile(v.WorkspaceConfigPath(workspace), []byte(existing), 0o600))

		require.NoError(t, v.InjectWorkspace(workspace, testCfg))

		data := readJSONAt(t, v.WorkspaceConfigPath(workspace))
		servers := data["servers"].(map[string]any)
		assert.Contains(t, servers, "github")
		assert.Contains(t, servers, "safedep")
	})

	t.Run("RemoveWorkspace removes safedep entry", func(t *testing.T) {
		workspace := t.TempDir()
		v := newVSCode(t.TempDir())

		require.NoError(t, v.InjectWorkspace(workspace, testCfg))
		require.NoError(t, v.RemoveWorkspace(workspace))

		data := readJSONAt(t, v.WorkspaceConfigPath(workspace))
		servers, _ := data["servers"].(map[string]any)
		assert.NotContains(t, servers, "safedep")
	})

	t.Run("RemoveWorkspace is no-op on absent file", func(t *testing.T) {
		require.NoError(t, newVSCode(t.TempDir()).RemoveWorkspace(t.TempDir()))
	})

	t.Run("RemoveWorkspace returns error on invalid JSON", func(t *testing.T) {
		workspace := t.TempDir()
		v := newVSCode(t.TempDir())
		path := v.WorkspaceConfigPath(workspace)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))
		require.Error(t, v.RemoveWorkspace(workspace))
	})

	t.Run("GlobalConfigured reads the servers key", func(t *testing.T) {
		v := newVSCode(t.TempDir())

		got, err := v.GlobalConfigured()
		require.NoError(t, err)
		assert.False(t, got)

		require.NoError(t, v.InjectGlobal(testCfg))
		got, err = v.GlobalConfigured()
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("GlobalConfigured returns error on invalid JSON", func(t *testing.T) {
		v := newVSCode(t.TempDir())
		path := v.GlobalConfigPath()
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))
		_, err := v.GlobalConfigured()
		require.Error(t, err)
	})

	t.Run("WorkspaceConfigured reads the servers key", func(t *testing.T) {
		workspace := t.TempDir()
		v := newVSCode(t.TempDir())

		require.NoError(t, v.InjectWorkspace(workspace, testCfg))
		got, err := v.WorkspaceConfigured(workspace)
		require.NoError(t, err)
		assert.True(t, got)
	})
}
