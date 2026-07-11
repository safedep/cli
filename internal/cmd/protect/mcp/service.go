package mcp

import (
	"errors"

	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/cli/internal/endpoint"
	"github.com/safedep/dry/cloud/endpointsync"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/steps"
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

type statusInput struct {
	WorkspaceDir string
}

// scopeStatus is the SafeDep MCP state for one config scope (global or
// workspace) of an agent. Err records a probe failure for this scope, so the
// report can mark it distinctly instead of conflating it with "not configured".
type scopeStatus struct {
	Supported  bool
	Configured bool
	Path       string
	Err        error
}

// agentStatus is the SafeDep MCP integration state of a single agent.
type agentStatus struct {
	Name      string
	Detected  bool
	Global    scopeStatus
	Workspace scopeStatus
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

	flow := steps.New(len(configurable))
	for _, a := range configurable {
		flow.Step("Configuring %s", a.Name())
	}

	if err := agent.InjectAll(configurable, cfg, in.WorkspaceDir); err != nil {
		return err
	}

	flow.Done("SafeDep MCP server configured for %d agent(s).", len(configurable))
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

// status reports the SafeDep MCP integration state of every known agent.
// Workspace scope is only inspected when WorkspaceDir is set. Per-agent read
// errors are accumulated and returned alongside the partial report.
func (s *mcpService) status(in statusInput) ([]agentStatus, error) {
	var (
		out  []agentStatus
		errs []error
	)

	for _, a := range s.agents {
		st := agentStatus{Name: a.Name(), Detected: a.Detected()}

		if gi, ok := a.AsGlobalInjector(); ok {
			st.Global.Supported = true
			st.Global.Path = gi.GlobalConfigPath()
			if st.Detected {
				configured, err := gi.GlobalConfigured()
				if err != nil {
					errs = append(errs, err)
					st.Global.Err = err
				}
				st.Global.Configured = configured
			}
		}

		if wi, ok := a.AsWorkspaceInjector(); ok && in.WorkspaceDir != "" {
			st.Workspace.Supported = true
			st.Workspace.Path = wi.WorkspaceConfigPath(in.WorkspaceDir)
			if st.Detected {
				configured, err := wi.WorkspaceConfigured(in.WorkspaceDir)
				if err != nil {
					errs = append(errs, err)
					st.Workspace.Err = err
				}
				st.Workspace.Configured = configured
			}
		}

		out = append(out, st)
	}

	return out, errors.Join(errs...)
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
