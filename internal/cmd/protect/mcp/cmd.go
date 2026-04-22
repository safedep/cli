package mcp

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage SafeDep MCP server configuration in AI IDEs",
	}

	cmd.AddCommand(installCmd(a))
	cmd.AddCommand(uninstallCmd(a))
	cmd.AddCommand(statusCmd(a))

	parent.AddCommand(cmd)
}
