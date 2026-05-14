package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// vsCodeMCPServerEntry is the entry format for VS Code's mcp.json.
// VS Code uses "servers" (not "mcpServers"), "type": "http", and "url".
type vsCodeMCPServerEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type vsCode struct {
	homeDir string
}

func newVSCode(homeDir string) *vsCode {
	return &vsCode{homeDir: homeDir}
}

func (v *vsCode) Name() string { return "vscode" }

func (v *vsCode) Detected() bool {
	// WSL2 Remote: ~/.vscode-server/ is created by the VS Code Remote extension.
	if _, err := os.Stat(filepath.Join(v.homeDir, ".vscode-server")); err == nil {
		return true
	}
	// Native Linux: ~/.config/Code/User/ is created by VS Code.
	_, err := os.Stat(filepath.Join(v.homeDir, ".config", "Code", "User"))
	return err == nil
}

// AsGlobalInjector returns false: VS Code's global user mcp.json lives in a
// platform-specific directory (Windows AppData on WSL2, XDG data dir on Linux)
// that cannot be reliably resolved from the CLI. Workspace injection is used instead.
func (v *vsCode) AsGlobalInjector() (GlobalInjector, bool)       { return nil, false }
func (v *vsCode) AsWorkspaceInjector() (WorkspaceInjector, bool) { return v, true }

// WorkspaceConfigPath returns .vscode/mcp.json within the workspace.
func (v *vsCode) WorkspaceConfigPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".vscode", "mcp.json")
}

func (v *vsCode) InjectWorkspace(workspaceDir string, cfg MCPConfig) error {
	return writeVSCodeMCPConfig(v.WorkspaceConfigPath(workspaceDir), cfg)
}

func (v *vsCode) RemoveWorkspace(workspaceDir string) error {
	return removeVSCodeMCPConfig(v.WorkspaceConfigPath(workspaceDir))
}

// writeVSCodeMCPConfig writes the SafeDep entry into a VS Code mcp.json.
// VS Code uses "servers" as the root key (not "mcpServers") and requires
// "type": "http" with "url" for remote Streamable HTTP servers.
func writeVSCodeMCPConfig(path string, cfg MCPConfig) error {
	data, err := readJSONFile(path)
	if err != nil {
		return err
	}

	servers, err := ensureVSCodeServers(data)
	if err != nil {
		return err
	}

	servers[safedepMCPKey] = vsCodeMCPServerEntry{
		Type:    "http",
		URL:     cfg.URL,
		Headers: cfg.Headers,
	}
	data["servers"] = servers

	return writeJSONFile(path, data)
}

// removeVSCodeMCPConfig removes the SafeDep entry from a VS Code mcp.json.
// No-op if the file or entry is absent.
func removeVSCodeMCPConfig(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("agent: parse %s: %w", path, err)
	}

	servers, ok := data["servers"].(map[string]any)
	if !ok {
		return nil
	}

	delete(servers, safedepMCPKey)
	data["servers"] = servers

	return writeJSONFile(path, data)
}

// ensureVSCodeServers returns the "servers" sub-map from data, creating it
// if absent. Returns an error if the existing value is not an object.
func ensureVSCodeServers(data map[string]any) (map[string]any, error) {
	v, ok := data["servers"]
	if !ok || v == nil {
		return make(map[string]any), nil
	}

	servers, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("agent: vscode: servers is not an object")
	}

	return servers, nil
}

var _ Agent = (*vsCode)(nil)
