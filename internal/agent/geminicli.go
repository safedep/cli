package agent

import (
	"os"
	"path/filepath"
)

type geminiCLI struct {
	homeDir string
}

func newGeminiCLI(homeDir string) *geminiCLI {
	return &geminiCLI{homeDir: homeDir}
}

func (g *geminiCLI) Name() string { return "gemini-cli" }

func (g *geminiCLI) Detected() bool {
	_, err := os.Stat(filepath.Join(g.homeDir, ".gemini"))
	return err == nil
}

func (g *geminiCLI) AsGlobalInjector() (GlobalInjector, bool)       { return g, true }
func (g *geminiCLI) AsWorkspaceInjector() (WorkspaceInjector, bool) { return g, true }

func (g *geminiCLI) GlobalConfigPath() string {
	return filepath.Join(g.homeDir, ".gemini", "settings.json")
}

func (g *geminiCLI) InjectGlobal(cfg MCPConfig) error {
	return writeMCPConfig(g.GlobalConfigPath(), cfg)
}

func (g *geminiCLI) RemoveGlobal() error {
	return removeMCPConfig(g.GlobalConfigPath())
}

func (g *geminiCLI) WorkspaceConfigPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".gemini", "settings.json")
}

func (g *geminiCLI) InjectWorkspace(workspaceDir string, cfg MCPConfig) error {
	return writeMCPConfig(g.WorkspaceConfigPath(workspaceDir), cfg)
}

func (g *geminiCLI) RemoveWorkspace(workspaceDir string) error {
	return removeMCPConfig(g.WorkspaceConfigPath(workspaceDir))
}

func (g *geminiCLI) GlobalConfigured() (bool, error) {
	return mcpEntryConfigured(g.GlobalConfigPath(), "mcpServers")
}

func (g *geminiCLI) WorkspaceConfigured(workspaceDir string) (bool, error) {
	return mcpEntryConfigured(g.WorkspaceConfigPath(workspaceDir), "mcpServers")
}

var _ Agent = (*geminiCLI)(nil)
