package agent

import "errors"

// InjectAll injects the SafeDep MCP config into every detected agent.
// workspaceDir="" skips workspace injection. Best-effort: all agents
// are attempted; errors are accumulated.
func InjectAll(agents []Agent, cfg MCPConfig, workspaceDir string) error {
	var errs []error

	for _, a := range agents {
		if !a.Detected() {
			continue
		}

		if inj, ok := a.AsGlobalInjector(); ok {
			if err := inj.InjectGlobal(cfg); err != nil {
				errs = append(errs, err)
			}
		}

		if workspaceDir != "" {
			if inj, ok := a.AsWorkspaceInjector(); ok {
				if err := inj.InjectWorkspace(workspaceDir, cfg); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	return errors.Join(errs...)
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
