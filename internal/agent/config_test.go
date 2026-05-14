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
	URL: "https://mcp.safedep.io/model-context-protocol/threats/v1/mcp",
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

// readJSONAt is a test helper shared across all adapter tests in this package.
func readJSONAt(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(raw, &data))
	return data
}
