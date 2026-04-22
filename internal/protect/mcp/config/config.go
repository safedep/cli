package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MCPServerEntry is the safedep entry written into each IDE's mcpServers block.
type MCPServerEntry struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// Read loads the raw JSON config at path. Returns an empty map if the file
// does not exist.
func Read(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("mcp config: read %s: %w", path, err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("mcp config: parse %s: %w", path, err)
	}

	return cfg, nil
}

// Merge upserts the safedep entry in cfg["mcpServers"] and returns the
// modified map. Does not write to disk.
func Merge(cfg map[string]any, serverName string, entry MCPServerEntry) map[string]any {
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[serverName] = entry
	cfg["mcpServers"] = servers
	return cfg
}

// Remove deletes the named entry from cfg["mcpServers"]. No-op if not present.
func Remove(cfg map[string]any, serverName string) map[string]any {
	if servers, ok := cfg["mcpServers"].(map[string]any); ok {
		delete(servers, serverName)
		cfg["mcpServers"] = servers
	}
	return cfg
}

// Write atomically writes cfg as pretty JSON to path, backing up the
// existing file first. Creates parent directories as needed.
func Write(path string, cfg map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mcp config: mkdir: %w", err)
	}

	if _, err := os.Stat(path); err == nil {
		backupPath := path + ".bak." + time.Now().Format("20060102150405")
		if err := os.Rename(path, backupPath); err != nil {
			return fmt.Errorf("mcp config: backup: %w", err)
		}
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("mcp config: marshal: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("mcp config: write tmp: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("mcp config: rename: %w", err)
	}

	return nil
}

// HasEntry reports whether cfg["mcpServers"][serverName] exists.
func HasEntry(cfg map[string]any, serverName string) bool {
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = servers[serverName]
	return ok
}
