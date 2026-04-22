package adapter

import (
	"context"
	"os"
	"path/filepath"

	mcpconfig "github.com/safedep/cli/internal/protect/mcp/config"
)

type windsurfAdapter struct{}

func (a *windsurfAdapter) Name() string        { return "windsurf" }
func (a *windsurfAdapter) DisplayName() string { return "Windsurf" }

func (a *windsurfAdapter) configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
}

func (a *windsurfAdapter) Detect(_ context.Context) (*DetectionResult, error) {
	path := a.configPath()
	dir := filepath.Dir(path)

	if _, err := os.Stat(dir); err != nil {
		return &DetectionResult{Found: false}, nil
	}

	return &DetectionResult{Found: true, ConfigPath: path}, nil
}

func (a *windsurfAdapter) Install(_ context.Context, creds MCPCredentials) error {
	path := a.configPath()

	cfg, err := mcpconfig.Read(path)
	if err != nil {
		return err
	}

	cfg = mcpconfig.Merge(cfg, mcpServerName, buildEntry(creds))
	return mcpconfig.Write(path, cfg)
}

func (a *windsurfAdapter) Uninstall(_ context.Context) error {
	path := a.configPath()

	cfg, err := mcpconfig.Read(path)
	if err != nil {
		return err
	}

	cfg = mcpconfig.Remove(cfg, mcpServerName)
	return mcpconfig.Write(path, cfg)
}

func (a *windsurfAdapter) Status(_ context.Context) (*MCPStatus, error) {
	path := a.configPath()

	cfg, err := mcpconfig.Read(path)
	if err != nil {
		return &MCPStatus{ConfigPath: path}, err
	}

	installed := mcpconfig.HasEntry(cfg, mcpServerName)
	return &MCPStatus{
		Installed:  installed,
		Valid:      installed,
		ConfigPath: path,
	}, nil
}
