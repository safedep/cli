package setup

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/setup/mcp"
	"github.com/spf13/cobra"
)

// Register adds the setup command tree to root.
func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "setup",
		Short: "Set up SafeDep for first-time use",
		Long: "Guided onboarding that authenticates with SafeDep Cloud, provisions an API key, " +
			"and configures AI coding agents on this machine in a single flow. " +
			"Composes auth + MCP configuration so new users need only one command.",
	}
	mcp.Register(parent, a)
	root.AddCommand(parent)
}
