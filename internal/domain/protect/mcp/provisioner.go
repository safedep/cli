package mcp

import (
	"context"
	"fmt"

	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/safedep/dry/cloud"
)

// ProvisionResult reports how many IDEs were configured and any per-IDE warnings.
type ProvisionResult struct {
	Installed int
	Warnings  []string
}

// DeprovisionResult reports how many IDEs were cleaned and any per-IDE warnings.
type DeprovisionResult struct {
	Removed  int
	Warnings []string
}

// Provisioner installs and removes the SafeDep MCP server entry across IDE adapters.
type Provisioner struct{}

// Provision resolves MCPCredentials from creds and injects them into every detected adapter.
func (p *Provisioner) Provision(ctx context.Context, adapters []adapter.MCPAdapter, creds *cloud.Credentials) (*ProvisionResult, error) {
	apiKey, err := creds.GetAPIKey()
	if err != nil {
		return nil, err
	}

	tenantDomain, err := creds.GetTenantDomain()
	if err != nil {
		return nil, err
	}

	mcpCreds := adapter.MCPCredentials{
		APIKey:   apiKey,
		TenantID: tenantDomain,
	}

	result := &ProvisionResult{}
	for _, ad := range adapters {
		detection, err := ad.Detect(ctx)
		if err != nil || !detection.Found {
			continue
		}
		if err := ad.Install(ctx, mcpCreds); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", ad.DisplayName(), err))
			continue
		}
		result.Installed++
	}

	return result, nil
}

// Deprovision removes the SafeDep MCP entry from every detected adapter.
func (p *Provisioner) Deprovision(ctx context.Context, adapters []adapter.MCPAdapter) (*DeprovisionResult, error) {
	result := &DeprovisionResult{}
	for _, ad := range adapters {
		detection, err := ad.Detect(ctx)
		if err != nil || !detection.Found {
			continue
		}
		if err := ad.Uninstall(ctx); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", ad.DisplayName(), err))
			continue
		}
		result.Removed++
	}

	return result, nil
}
