package agent

import (
	"os"
	"path/filepath"
)

type cursor struct {
	homeDir string
}

func newCursor(homeDir string) *cursor {
	return &cursor{homeDir: homeDir}
}

func (c *cursor) Name() string { return "cursor" }

func (c *cursor) Detected() bool {
	_, err := os.Stat(filepath.Join(c.homeDir, ".cursor"))
	return err == nil
}

func (c *cursor) AsGlobalInjector() (GlobalInjector, bool)       { return c, true }
func (c *cursor) AsWorkspaceInjector() (WorkspaceInjector, bool) { return c, true }

func (c *cursor) GlobalConfigPath() string {
	return filepath.Join(c.homeDir, ".cursor", "mcp.json")
}

func (c *cursor) InjectGlobal(cfg MCPConfig) error {
	return writeMCPConfig(c.GlobalConfigPath(), cfg)
}

func (c *cursor) RemoveGlobal() error {
	return removeMCPConfig(c.GlobalConfigPath())
}

func (c *cursor) WorkspaceConfigPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".cursor", "mcp.json")
}

func (c *cursor) InjectWorkspace(workspaceDir string, cfg MCPConfig) error {
	return writeMCPConfig(c.WorkspaceConfigPath(workspaceDir), cfg)
}

func (c *cursor) RemoveWorkspace(workspaceDir string) error {
	return removeMCPConfig(c.WorkspaceConfigPath(workspaceDir))
}

var _ Agent = (*cursor)(nil)
