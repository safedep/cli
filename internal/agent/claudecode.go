package agent

import (
	"os"
	"path/filepath"
)

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

func (c *claudeCode) GlobalConfigPath() string {
	return filepath.Join(c.homeDir, ".claude", "settings.json")
}

func (c *claudeCode) InjectGlobal(cfg MCPConfig) error {
	return writeMCPConfig(c.GlobalConfigPath(), cfg)
}

func (c *claudeCode) RemoveGlobal() error {
	return removeMCPConfig(c.GlobalConfigPath())
}

func (c *claudeCode) WorkspaceConfigPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".claude", "settings.json")
}

func (c *claudeCode) InjectWorkspace(workspaceDir string, cfg MCPConfig) error {
	return writeMCPConfig(c.WorkspaceConfigPath(workspaceDir), cfg)
}

func (c *claudeCode) RemoveWorkspace(workspaceDir string) error {
	return removeMCPConfig(c.WorkspaceConfigPath(workspaceDir))
}

var _ Agent = (*claudeCode)(nil)
