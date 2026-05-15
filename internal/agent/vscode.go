package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	_, err := os.Stat(v.userConfigDir())
	return err == nil
}

func (v *vsCode) AsGlobalInjector() (GlobalInjector, bool)       { return v, true }
func (v *vsCode) AsWorkspaceInjector() (WorkspaceInjector, bool) { return v, true }

// GlobalConfigPath returns the user-level mcp.json path for the current OS:
//   - Linux/WSL2: ~/.config/Code/User/mcp.json
//   - macOS:      ~/Library/Application Support/Code/User/mcp.json
//   - Windows:    ~\AppData\Roaming\Code\User\mcp.json
func (v *vsCode) GlobalConfigPath() string {
	return filepath.Join(v.userConfigDir(), "mcp.json")
}

// userConfigDir returns the VS Code user config directory for the current OS.
func (v *vsCode) userConfigDir() string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(v.homeDir, "Library", "Application Support", "Code", "User")
	case "windows":
		return filepath.Join(v.homeDir, "AppData", "Roaming", "Code", "User")
	default: // linux, freebsd, etc.
		return filepath.Join(v.homeDir, ".config", "Code", "User")
	}
}

func (v *vsCode) InjectGlobal(cfg MCPConfig) error {
	return writeVSCodeMCPConfig(v.GlobalConfigPath(), cfg)
}

func (v *vsCode) RemoveGlobal() error {
	return removeVSCodeMCPConfig(v.GlobalConfigPath())
}

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
	if len(raw) == 0 {
		return nil
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
