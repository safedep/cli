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

	configurable := s.configurableAgents(in.WorkspaceDir)
	if len(configurable) == 0 {
		tui.Warning("No supported AI agents detected on this machine.")
		return nil
	}

	for _, a := range configurable {
		tui.Info("Configuring %s", a.Name())
	}

	if err := agent.InjectAll(configurable, cfg, in.WorkspaceDir); err != nil {
		return err
	}

	tui.Success("SafeDep MCP server configured for %d agent(s).", len(configurable))
	return nil
}

func (s *mcpService) uninstall(in uninstallInput) error {
	configurable := s.configurableAgents(in.WorkspaceDir)
	if len(configurable) == 0 {
		tui.Warning("No supported AI agents detected on this machine.")
		return nil
	}

	if err := agent.RemoveAll(configurable, in.WorkspaceDir); err != nil {
		return err
	}

	tui.Success("SafeDep MCP server configuration removed from %d agent(s).", len(configurable))
	return nil
}

// configurableAgents returns detected agents that have at least one applicable
// injector given workspaceDir. Agents detected but with no injectors applicable
// to the current invocation (e.g. workspace-only agents called without
// --workspace) are excluded so they are not logged or counted.
func (s *mcpService) configurableAgents(workspaceDir string) []agent.Agent {
	var result []agent.Agent
	for _, a := range s.agents {
		if !a.Detected() {
			continue
		}
		_, hasGlobal := a.AsGlobalInjector()
		_, hasWorkspace := a.AsWorkspaceInjector()
		if hasGlobal || (hasWorkspace && workspaceDir != "") {
			result = append(result, a)
		}
	}
	return result
}
