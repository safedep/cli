package agent

import (
	"errors"
	"maps"
)

// AgentNameHeader carries the agent slug (e.g. "claude-code") to the MCP
// server, which uses it to attribute requests to a specific agent.
const AgentNameHeader = "X-Agent-Name"

// InjectAll injects the SafeDep MCP config into every detected agent.
// workspaceDir="" skips workspace injection. Best-effort: all agents
// are attempted; errors are accumulated.
func InjectAll(agents []Agent, cfg MCPConfig, workspaceDir string) error {
	var errs []error

	for _, a := range agents {
		if !a.Detected() {
			continue
		}

		// Clone the headers so each agent gets its own name without mutating
		// the caller's shared map.
		ac := withAgentName(cfg, a.Name())

		if inj, ok := a.AsGlobalInjector(); ok {
			if err := inj.InjectGlobal(ac); err != nil {
				errs = append(errs, err)
			}
		}

		if workspaceDir != "" {
			if inj, ok := a.AsWorkspaceInjector(); ok {
				if err := inj.InjectWorkspace(workspaceDir, ac); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	return errors.Join(errs...)
}

// withAgentName returns a copy of cfg with AgentNameHeader set to name,
// cloning Headers so the caller's map is never mutated.
func withAgentName(cfg MCPConfig, name string) MCPConfig {
	h := make(map[string]string, len(cfg.Headers)+1)
	maps.Copy(h, cfg.Headers)
	h[AgentNameHeader] = name
	cfg.Headers = h
	return cfg
}

// RemoveAll removes the SafeDep MCP config from every detected agent.
// Same error semantics as InjectAll.
func RemoveAll(agents []Agent, workspaceDir string) error {
	var errs []error

	for _, a := range agents {
		if !a.Detected() {
			continue
		}

		if inj, ok := a.AsGlobalInjector(); ok {
			if err := inj.RemoveGlobal(); err != nil {
				errs = append(errs, err)
			}
		}

		if workspaceDir != "" {
			if inj, ok := a.AsWorkspaceInjector(); ok {
				if err := inj.RemoveWorkspace(workspaceDir); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	return errors.Join(errs...)
}
