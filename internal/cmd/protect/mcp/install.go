package mcp

import (
	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/cloud/endpointsync"
	"github.com/spf13/cobra"
)

const defaultMCPServerURL = "https://mcp.safedep.io/model-context-protocol/threats/v1/mcp"

type installFlags struct {
	MCPURL       string
	WorkspaceDir string
}

func installCmd(a *app.App) *cobra.Command {
	var flags installFlags

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install SafeDep MCP server configuration into AI agents",
		Long: "Detect AI coding agents installed on this machine and inject the SafeDep MCP " +
			"server entry into each agent's config file. Requires an authenticated session " +
			"(run `safedep auth login` first). Pass --workspace to also inject into the current project.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolver, err := a.APIKeyResolver()
			if err != nil {
				return err
			}

			creds, err := resolver.Resolve()
			if err != nil {
				return err
			}

			apiKey, err := creds.GetAPIKey()
			if err != nil {
				return err
			}

			tenantID, err := creds.GetTenantDomain()
			if err != nil {
				return err
			}

			svc := newMCPService(agent.NewRegistry(), endpointsync.NewEndpointIdentityResolver())

			return svc.install(installInput{
				MCPURL:       flags.MCPURL,
				APIKey:       apiKey,
				TenantID:     tenantID,
				WorkspaceDir: flags.WorkspaceDir,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&flags.MCPURL, "mcp-url", defaultMCPServerURL, "SafeDep MCP server URL")
	f.StringVar(&flags.WorkspaceDir, "workspace", "", "project directory for workspace-level injection (empty = skip)")

	return cmd
}
