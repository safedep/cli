package mcp

import (
	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/cli/internal/endpoint"
	"github.com/safedep/dry/cloud/endpointsync"
	"github.com/safedep/dry/tui"
)

type installInput struct {
	MCPURL       string
	APIKey       string
	TenantID     string
	WorkspaceDir string
}

type uninstallInput struct {
	WorkspaceDir string
}

type mcpService struct {
	agents   []agent.Agent
	resolver endpointsync.EndpointIdentityResolver
}

func newMCPService(agents []agent.Agent, resolver endpointsync.EndpointIdentityResolver) *mcpService {
	return &mcpService{agents: agents, resolver: resolver}
}

func (s *mcpService) install(in installInput) error {
	cfg, err := endpoint.BuildMCPConfig(in.MCPURL, in.APIKey, in.TenantID, s.resolver)
	if err != nil {
		return err
	}

	detected := agent.FilterDetected(s.agents)
	if len(detected) == 0 {
		tui.Warning("No supported AI agents detected on this machine.")
		return nil
	}

	for _, a := range detected {
		tui.Info("Configuring %s", a.Name())
	}

	if err := agent.InjectAll(detected, cfg, in.WorkspaceDir); err != nil {
		return err
	}

	tui.Success("SafeDep MCP server configured for %d agent(s).", len(detected))
	return nil
}

func (s *mcpService) uninstall(in uninstallInput) error {
	detected := agent.FilterDetected(s.agents)
	if len(detected) == 0 {
		tui.Warning("No supported AI agents detected on this machine.")
		return nil
	}

	if err := agent.RemoveAll(detected, in.WorkspaceDir); err != nil {
		return err
	}

	tui.Success("SafeDep MCP server configuration removed from %d agent(s).", len(detected))
	return nil
}
