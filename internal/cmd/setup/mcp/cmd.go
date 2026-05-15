package mcp

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register adds the mcp sub-command tree to parent (the setup command).
func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Set up SafeDep MCP server configuration",
		Long: "Authenticate with SafeDep Cloud and inject the SafeDep MCP server into the " +
			"configuration files of AI coding agents detected on this machine. " +
			"Handles the full onboarding flow: device login, API key creation, and agent config injection.",
	}
	cmd.AddCommand(installCmd(a))
	parent.AddCommand(cmd)
}
