package agent

// MCPConfig is the SafeDep MCP server entry to inject.
type MCPConfig struct {
	URL     string
	Headers map[string]string
}

// Agent is an AI coding agent that may be installed on the current machine.
type Agent interface {
	// Name returns a stable identifier (e.g. "claude-code").
	Name() string

	// Detected reports whether the agent is installed on this machine.
	Detected() bool

	// AsGlobalInjector returns the user-level injector if this agent supports global config.
	AsGlobalInjector() (GlobalInjector, bool)

	// AsWorkspaceInjector returns the workspace injector if this agent supports project config.
	AsWorkspaceInjector() (WorkspaceInjector, bool)
}

// GlobalInjector writes or removes the SafeDep MCP config from a user-level config file.
type GlobalInjector interface {
	// GlobalConfigPath returns the absolute path to the global config file.
	GlobalConfigPath() string

	// InjectGlobal writes the SafeDep entry. Idempotent; preserves all other keys.
	InjectGlobal(cfg MCPConfig) error

	// RemoveGlobal deletes the SafeDep entry. No-op if absent.
	RemoveGlobal() error

	// GlobalConfigured reports whether the SafeDep entry is present in the
	// global config file. Returns false (no error) when the file is absent.
	GlobalConfigured() (bool, error)
}

// WorkspaceInjector writes or removes the SafeDep MCP config from a workspace config file.
type WorkspaceInjector interface {
	// WorkspaceConfigPath returns the absolute path to the workspace config file.
	// The path may or may not be inside workspaceDir depending on the agent.
	WorkspaceConfigPath(workspaceDir string) string

	// InjectWorkspace writes the SafeDep entry. Idempotent; preserves all other keys.
	InjectWorkspace(workspaceDir string, cfg MCPConfig) error

	// RemoveWorkspace deletes the SafeDep entry. No-op if absent.
	RemoveWorkspace(workspaceDir string) error

	// WorkspaceConfigured reports whether the SafeDep entry is present in the
	// workspace config file. Returns false (no error) when the file is absent.
	WorkspaceConfigured(workspaceDir string) (bool, error)
}
