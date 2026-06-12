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

	servers[safedepMCPKey] = mcpServerEntry(cfg)
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
	if len(raw) == 0 {
		return nil
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

// mcpEntryConfigured reports whether the SafeDep entry exists under
// rootKey.safedep in the JSON config file at path. Returns false (no error)
// when the file is absent or empty. rootKey is the agent-specific container
// key (e.g. "mcpServers", "servers", "mcp"). Server bodies are decoded as
// json.RawMessage so only the structure needed to detect the entry is parsed.
func mcpEntryConfigured(path, rootKey string) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(raw) == 0 {
		return false, nil
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, fmt.Errorf("agent: parse %s: %w", path, err)
	}

	container, ok := doc[rootKey]
	if !ok {
		return false, nil
	}

	var servers map[string]json.RawMessage
	if err := json.Unmarshal(container, &servers); err != nil {
		return false, fmt.Errorf("agent: parse %s: %w", path, err)
	}

	_, ok = servers[safedepMCPKey]
	return ok, nil
}

// readJSONFile reads and unmarshals a JSON file. Returns an empty map when
// the file does not exist or is empty so the caller can create one from scratch.
func readJSONFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return make(map[string]any), nil
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
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	return os.Rename(tmpName, path)
}
