package mcp

import (
	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/cloud/endpointsync"
	"github.com/spf13/cobra"
)

type uninstallFlags struct {
	WorkspaceDir string
}

func uninstallCmd(a *app.App) *cobra.Command {
	var flags uninstallFlags

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove SafeDep MCP server configuration from AI agents",
		Long: "Remove the SafeDep MCP server entry from the configuration files of all AI " +
			"coding agents detected on this machine. Does not require authentication.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := newMCPService(agent.NewRegistry(), endpointsync.NewEndpointIdentityResolver())

			return svc.uninstall(uninstallInput(flags))
		},
	}

	cmd.Flags().StringVar(&flags.WorkspaceDir, "workspace", "", "project directory for workspace-level removal (empty = skip)")

	return cmd
}
