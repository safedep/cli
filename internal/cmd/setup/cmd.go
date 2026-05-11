package setup

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/setup/mcp"
	"github.com/spf13/cobra"
)

// Register wires the setup command tree onto root.
func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "setup",
		Short: "Set up SafeDep integrations and tooling",
		Long:  "Configure integrations with SafeDep Cloud, including AI agent MCP server installation.",
	}
	mcp.Register(parent, a)
	root.AddCommand(parent)
}
