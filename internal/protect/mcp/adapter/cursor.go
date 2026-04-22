package adapter

import (
	"context"
	"os"
	"path/filepath"

	mcpconfig "github.com/safedep/cli/internal/protect/mcp/config"
)

type cursorAdapter struct{}

func (a *cursorAdapter) Name() string        { return "cursor" }
func (a *cursorAdapter) DisplayName() string { return "Cursor" }

func (a *cursorAdapter) configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cursor", "mcp.json")
}

func (a *cursorAdapter) Detect(_ context.Context) (*DetectionResult, error) {
	path := a.configPath()
	dir := filepath.Dir(path)

	if _, err := os.Stat(dir); err != nil {
		return &DetectionResult{Found: false}, nil
	}

	return &DetectionResult{Found: true, ConfigPath: path}, nil
}

func (a *cursorAdapter) Install(_ context.Context, creds MCPCredentials) error {
	path := a.configPath()

	cfg, err := mcpconfig.Read(path)
	if err != nil {
		return err
	}

	cfg = mcpconfig.Merge(cfg, mcpServerName, buildEntry(creds))
	return mcpconfig.Write(path, cfg)
}

func (a *cursorAdapter) Uninstall(_ context.Context) error {
	path := a.configPath()

	cfg, err := mcpconfig.Read(path)
	if err != nil {
		return err
	}

	cfg = mcpconfig.Remove(cfg, mcpServerName)
	return mcpconfig.Write(path, cfg)
}

func (a *cursorAdapter) Status(_ context.Context) (*MCPStatus, error) {
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
