package adapter

import "context"

const (
	mcpServerName = "safedep"
	mcpServerURL  = "https://mcp.safedep.io/model-context-protocol/threats/v1"
)

// MCPCredentials holds the credentials written into each IDE's MCP config.
// EndpointID is empty until a device registration API exists; adapters
// include it only when non-empty.
type MCPCredentials struct {
	APIKey     string
	TenantID   string
	EndpointID string // optional; populated when device registration API lands
}

// DetectionResult reports whether an IDE is installed on this machine.
type DetectionResult struct {
	Found      bool
	ConfigPath string
}

// MCPStatus reports whether the SafeDep MCP entry is present and valid.
type MCPStatus struct {
	Installed bool
	Valid     bool   // entry present and credentials match current stored creds
	ConfigPath string
}

// MCPAdapter is implemented by each supported AI IDE.
type MCPAdapter interface {
	Name() string
	DisplayName() string
	Detect(ctx context.Context) (*DetectionResult, error)
	Install(ctx context.Context, creds MCPCredentials) error
	Uninstall(ctx context.Context) error
	Status(ctx context.Context) (*MCPStatus, error)
}
