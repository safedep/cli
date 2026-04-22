package adapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	mcpconfig "github.com/safedep/cli/internal/protect/mcp/config"
)

type claudeCodeAdapter struct{}

func (a *claudeCodeAdapter) Name() string        { return "claude-code" }
func (a *claudeCodeAdapter) DisplayName() string { return "Claude Code" }

func (a *claudeCodeAdapter) configPath() string {
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "Claude", "claude_desktop_config.json")
		}
	}

	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "claude_desktop_config.json")
}

func (a *claudeCodeAdapter) Detect(_ context.Context) (*DetectionResult, error) {
	path := a.configPath()

	// Detect by config dir presence, since the binary lives in PATH variously.
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		return &DetectionResult{Found: false}, nil
	}

	return &DetectionResult{Found: true, ConfigPath: path}, nil
}

func (a *claudeCodeAdapter) Install(_ context.Context, creds MCPCredentials) error {
	path := a.configPath()

	cfg, err := mcpconfig.Read(path)
	if err != nil {
		return err
	}

	entry := buildEntry(creds)
	cfg = mcpconfig.Merge(cfg, mcpServerName, entry)
	return mcpconfig.Write(path, cfg)
}

func (a *claudeCodeAdapter) Uninstall(_ context.Context) error {
	path := a.configPath()

	cfg, err := mcpconfig.Read(path)
	if err != nil {
		return err
	}

	cfg = mcpconfig.Remove(cfg, mcpServerName)
	return mcpconfig.Write(path, cfg)
}

func (a *claudeCodeAdapter) Status(_ context.Context) (*MCPStatus, error) {
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

func buildEntry(creds MCPCredentials) mcpconfig.MCPServerEntry {
	headers := map[string]string{
		"Authorization": "Bearer " + creds.APIKey,
		"X-Tenant-ID":   creds.TenantID,
	}

	if creds.EndpointID != "" {
		headers["X-Endpoint-ID"] = creds.EndpointID
	}

	return mcpconfig.MCPServerEntry{
		URL:     mcpServerURL,
		Headers: headers,
	}
}

