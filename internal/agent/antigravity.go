package agent

import (
	"os"
	"path/filepath"
)

type antigravity struct {
	homeDir string
}

func newAntigravity(homeDir string) *antigravity {
	return &antigravity{homeDir: homeDir}
}

func (a *antigravity) Name() string { return "antigravity" }

func (a *antigravity) Detected() bool {
	_, err := os.Stat(filepath.Join(a.homeDir, ".gemini", "antigravity"))
	return err == nil
}

func (a *antigravity) AsGlobalInjector() (GlobalInjector, bool)       { return a, true }
func (a *antigravity) AsWorkspaceInjector() (WorkspaceInjector, bool) { return nil, false }

func (a *antigravity) GlobalConfigPath() string {
	return filepath.Join(a.homeDir, ".gemini", "antigravity", "mcp_config.json")
}

func (a *antigravity) InjectGlobal(cfg MCPConfig) error {
	return writeAntigravityMCPConfig(a.GlobalConfigPath(), cfg)
}

func (a *antigravity) RemoveGlobal() error {
	return removeMCPConfig(a.GlobalConfigPath())
}

// antigravityMCPServerEntry is the entry format for ~/.gemini/antigravity/mcp_config.json.
type antigravityMCPServerEntry struct {
	ServerURL string            `json:"serverUrl"`
	Headers   map[string]string `json:"headers,omitempty"`
}

func writeAntigravityMCPConfig(path string, cfg MCPConfig) error {
	data, err := readJSONFile(path)
	if err != nil {
		return err
	}

	servers, err := ensureMCPServers(data)
	if err != nil {
		return err
	}

	servers[safedepMCPKey] = antigravityMCPServerEntry{
		ServerURL: cfg.URL,
		Headers:   cfg.Headers,
	}
	data["mcpServers"] = servers

	return writeJSONFile(path, data)
}

var _ Agent = (*antigravity)(nil)
