package agent

import (
	"encoding/json"
	"errors"
	"fmt"
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

// WorkspaceConfigPath returns ~/.claude.json — Claude Code stores per-project
// MCP servers in the global file under projects[workspaceDir].mcpServers, not
// inside the workspace directory.
func (c *claudeCode) WorkspaceConfigPath(_ string) string {
	return c.GlobalConfigPath()
}

func (c *claudeCode) InjectWorkspace(workspaceDir string, cfg MCPConfig) error {
	key, err := claudeProjectKey(workspaceDir)
	if err != nil {
		return err
	}
	return writeClaudeCodeWorkspaceMCPConfig(c.GlobalConfigPath(), key, cfg)
}

func (c *claudeCode) RemoveWorkspace(workspaceDir string) error {
	key, err := claudeProjectKey(workspaceDir)
	if err != nil {
		return err
	}
	return removeClaudeCodeWorkspaceMCPConfig(c.GlobalConfigPath(), key)
}

// claudeProjectKey resolves workspaceDir to the absolute, cleaned path Claude
// Code uses as the projects map key in ~/.claude.json. A relative path like "."
// or one with a trailing slash would otherwise miss the entry Claude Code keys
// by absolute path, both when probing and when writing.
func claudeProjectKey(workspaceDir string) (string, error) {
	abs, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("agent: resolve workspace path %s: %w", workspaceDir, err)
	}
	return abs, nil
}

func (c *claudeCode) GlobalConfigured() (bool, error) {
	return mcpEntryConfigured(c.GlobalConfigPath(), "mcpServers")
}

// claudeCodeProbe is a minimal view of ~/.claude.json used to detect a
// per-project SafeDep entry. Only projects[*].mcpServers is parsed; server
// bodies stay raw.
type claudeCodeProbe struct {
	Projects map[string]struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	} `json:"projects"`
}

// WorkspaceConfigured checks projects[workspaceDir].mcpServers.safedep in
// ~/.claude.json, the per-project location Claude Code uses.
func (c *claudeCode) WorkspaceConfigured(workspaceDir string) (bool, error) {
	key, err := claudeProjectKey(workspaceDir)
	if err != nil {
		return false, err
	}

	raw, err := os.ReadFile(c.GlobalConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(raw) == 0 {
		return false, nil
	}

	var probe claudeCodeProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false, fmt.Errorf("agent: parse %s: %w", c.GlobalConfigPath(), err)
	}

	_, ok := probe.Projects[key].MCPServers[safedepMCPKey]
	return ok, nil
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

// writeClaudeCodeWorkspaceMCPConfig writes the SafeDep entry into
// ~/.claude.json under projects[workspaceDir].mcpServers, preserving all other
// keys.
func writeClaudeCodeWorkspaceMCPConfig(path, workspaceDir string, cfg MCPConfig) error {
	data, err := readJSONFile(path)
	if err != nil {
		return err
	}

	projects, err := ensureClaudeProjects(data)
	if err != nil {
		return err
	}

	project, err := ensureClaudeProject(projects, workspaceDir)
	if err != nil {
		return err
	}

	servers, err := ensureMCPServers(project)
	if err != nil {
		return err
	}

	servers[safedepMCPKey] = claudeCodeMCPServerEntry{
		Type:    "http",
		URL:     cfg.URL,
		Headers: cfg.Headers,
	}
	project["mcpServers"] = servers
	projects[workspaceDir] = project
	data["projects"] = projects

	return writeJSONFile(path, data)
}

// removeClaudeCodeWorkspaceMCPConfig removes the SafeDep entry from
// ~/.claude.json under projects[workspaceDir].mcpServers. No-op if absent.
func removeClaudeCodeWorkspaceMCPConfig(path, workspaceDir string) error {
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

	projects, ok := data["projects"].(map[string]any)
	if !ok {
		return nil
	}

	project, ok := projects[workspaceDir].(map[string]any)
	if !ok {
		return nil
	}

	servers, ok := project["mcpServers"].(map[string]any)
	if !ok {
		return nil
	}

	delete(servers, safedepMCPKey)
	project["mcpServers"] = servers
	projects[workspaceDir] = project
	data["projects"] = projects

	return writeJSONFile(path, data)
}

func ensureClaudeProjects(data map[string]any) (map[string]any, error) {
	v, ok := data["projects"]
	if !ok || v == nil {
		return make(map[string]any), nil
	}

	projects, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("agent: projects is not an object")
	}

	return projects, nil
}

func ensureClaudeProject(projects map[string]any, workspaceDir string) (map[string]any, error) {
	v, ok := projects[workspaceDir]
	if !ok || v == nil {
		return make(map[string]any), nil
	}

	project, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("agent: project %s is not an object", workspaceDir)
	}

	return project, nil
}

var _ Agent = (*claudeCode)(nil)
