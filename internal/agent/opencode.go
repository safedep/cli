package agent

import (
	"fmt"
	"os"
	"path/filepath"
)

type openCode struct {
	homeDir string
}

func newOpenCode(homeDir string) *openCode {
	return &openCode{homeDir: homeDir}
}

func (o *openCode) Name() string { return "opencode" }

func (o *openCode) Detected() bool {
	_, err := os.Stat(filepath.Join(o.homeDir, ".config", "opencode"))
	return err == nil
}

func (o *openCode) AsGlobalInjector() (GlobalInjector, bool)       { return o, true }
func (o *openCode) AsWorkspaceInjector() (WorkspaceInjector, bool) { return o, true }

func (o *openCode) GlobalConfigPath() string {
	return filepath.Join(o.homeDir, ".config", "opencode", "opencode.json")
}

func (o *openCode) InjectGlobal(cfg MCPConfig) error {
	return writeOpenCodeMCPConfig(o.GlobalConfigPath(), cfg)
}

func (o *openCode) RemoveGlobal() error {
	return removeOpenCodeMCPConfig(o.GlobalConfigPath())
}

func (o *openCode) WorkspaceConfigPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, "opencode.json")
}

func (o *openCode) InjectWorkspace(workspaceDir string, cfg MCPConfig) error {
	return writeOpenCodeMCPConfig(o.WorkspaceConfigPath(workspaceDir), cfg)
}

func (o *openCode) RemoveWorkspace(workspaceDir string) error {
	return removeOpenCodeMCPConfig(o.WorkspaceConfigPath(workspaceDir))
}

type openCodeMCPServerEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Enabled bool              `json:"enabled"`
	Headers map[string]string `json:"headers,omitempty"`
}

func writeOpenCodeMCPConfig(path string, cfg MCPConfig) error {
	data, err := readJSONFile(path)
	if err != nil {
		return err
	}

	servers, err := ensureOpenCodeMCP(data)
	if err != nil {
		return err
	}

	servers[safedepMCPKey] = openCodeMCPServerEntry{
		Type:    "remote",
		URL:     cfg.URL,
		Enabled: true,
		Headers: cfg.Headers,
	}
	data["mcp"] = servers

	return writeJSONFile(path, data)
}

func removeOpenCodeMCPConfig(path string) error {
	data, err := readJSONFile(path)
	if err != nil {
		return err
	}

	servers, ok := data["mcp"].(map[string]any)
	if !ok {
		return nil
	}

	delete(servers, safedepMCPKey)
	data["mcp"] = servers

	return writeJSONFile(path, data)
}

func ensureOpenCodeMCP(data map[string]any) (map[string]any, error) {
	v, ok := data["mcp"]
	if !ok || v == nil {
		return make(map[string]any), nil
	}

	servers, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("agent: mcp is not an object")
	}

	return servers, nil
}

var _ Agent = (*openCode)(nil)
