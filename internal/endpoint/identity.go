package endpoint

import (
	"encoding/base64"
	"fmt"

	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/dry/cloud/endpointsync"
	"google.golang.org/protobuf/proto"
)

// IdentityHeaderValue returns the X-Endpoint-ID header value for the current
// machine: base64(proto.Marshal(EndpointIdentity)). The identity is derived
// from the machine's stable hardware UUID and hostname; it does not require
// backend registration.
func IdentityHeaderValue(resolver endpointsync.EndpointIdentityResolver) (string, error) {
	identity, err := resolver.Resolve()
	if err != nil {
		return "", fmt.Errorf("endpoint: resolve identity: %w", err)
	}

	b, err := proto.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("endpoint: marshal identity: %w", err)
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

// BuildMCPConfig assembles the MCPConfig that agent.InjectAll expects.
// It populates all three SafeDep MCP headers:
//   - Authorization: Bearer <apiKey>
//   - X-Tenant-ID: <tenantID>
//   - X-Endpoint-ID: base64(proto.Marshal(EndpointIdentity))
func BuildMCPConfig(mcpURL, apiKey, tenantID string, resolver endpointsync.EndpointIdentityResolver) (agent.MCPConfig, error) {
	endpointID, err := IdentityHeaderValue(resolver)
	if err != nil {
		return agent.MCPConfig{}, err
	}

	return agent.MCPConfig{
		URL: mcpURL,
		Headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
			"X-Tenant-ID":   tenantID,
			"X-Endpoint-ID": endpointID,
		},
	}, nil
}
