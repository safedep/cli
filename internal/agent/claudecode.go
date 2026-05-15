package agent

import (
	"os"
	"path/filepath"
)

// claudeCodeMCPServerEntry is the entry format for ~/.claude.json.
// Claude Code requires "type": "http" for remote MCP servers.
type claudeCodeMCPServerEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type claudeCode struct {
	homeDir string
}

func newClaudeCode(homeDir string) *claudeCode {
	return &claudeCode{homeDir: homeDir}
}

func (c *claudeCode) Name() string { return "claude-code" }

func (c *claudeCode) Detected() bool {
	_, err := os.Stat(filepath.Join(c.homeDir, ".claude"))
	return err == nil
}

func (c *claudeCode) AsGlobalInjector() (GlobalInjector, bool)       { return c, true }
func (c *claudeCode) AsWorkspaceInjector() (WorkspaceInjector, bool) { return c, true }

// GlobalConfigPath returns ~/.claude.json — the file Claude Code uses for
// user-level MCP server configuration.
func (c *claudeCode) GlobalConfigPath() string {
	return filepath.Join(c.homeDir, ".claude.json")
}

func (c *claudeCode) InjectGlobal(cfg MCPConfig) error {
	return writeClaudeCodeMCPConfig(c.GlobalConfigPath(), cfg)
}

func (c *claudeCode) RemoveGlobal() error {
	return removeMCPConfig(c.GlobalConfigPath())
}

// WorkspaceConfigPath returns .claude/settings.json within the workspace —
// the project-scoped MCP config location for Claude Code.
func (c *claudeCode) WorkspaceConfigPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".claude", "settings.json")
}

func (c *claudeCode) InjectWorkspace(workspaceDir string, cfg MCPConfig) error {
	return writeMCPConfig(c.WorkspaceConfigPath(workspaceDir), cfg)
}

func (c *claudeCode) RemoveWorkspace(workspaceDir string) error {
	return removeMCPConfig(c.WorkspaceConfigPath(workspaceDir))
}

// writeClaudeCodeMCPConfig writes the SafeDep entry into a Claude Code JSON
// config file using the "type": "http" format required by ~/.claude.json.
func writeClaudeCodeMCPConfig(path string, cfg MCPConfig) error {
	data, err := readJSONFile(path)
	if err != nil {
		return err
	}

	servers, err := ensureMCPServers(data)
	if err != nil {
		return err
	}

	servers[safedepMCPKey] = claudeCodeMCPServerEntry{
		Type:    "http",
		URL:     cfg.URL,
		Headers: cfg.Headers,
	}
	data["mcpServers"] = servers

	return writeJSONFile(path, data)
}

var _ Agent = (*claudeCode)(nil)
