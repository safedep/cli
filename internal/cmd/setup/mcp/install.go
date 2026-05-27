package mcp

import (
	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/endpoint"
	"github.com/safedep/dry/cloud/endpointsync"
	"github.com/spf13/cobra"
)

type installFlags struct {
	MCPURL       string
	WorkspaceDir string
	Force        bool
}

func installCmd(a *app.App) *cobra.Command {
	var flags installFlags

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Authenticate and install SafeDep MCP server configuration into AI agents",
		Long: "Guided first-time onboarding: authenticates with SafeDep Cloud via OAuth2 device " +
			"flow, creates an API key, and injects the SafeDep MCP server entry into each " +
			"AI coding agent's config file detected on this machine. " +
			"Skip the auth flow when valid credentials already exist (use --force to re-authenticate).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := a.CredentialStore()
			if err != nil {
				return err
			}

			svc := &setupMCPService{
				agents:   agent.NewRegistry(),
				resolver: endpointsync.NewEndpointIdentityResolver(),
			}

			return svc.install(cmd.Context(), installInput{
				APIKeyResolver:  a.APIKeyResolver,
				CredentialStore: store,
				MCPURL:          flags.MCPURL,
				WorkspaceDir:    flags.WorkspaceDir,
				Force:           flags.Force,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&flags.MCPURL, "mcp-url", endpoint.DefaultMCPServerURL, "SafeDep MCP server URL")
	f.StringVar(&flags.WorkspaceDir, "workspace", "", "project directory for workspace-level injection (empty = skip)")
	f.BoolVar(&flags.Force, "force", false, "bypass credential check and always re-authenticate")

	return cmd
}
