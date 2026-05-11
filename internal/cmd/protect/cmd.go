package protect

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/protect/mcp"
	"github.com/spf13/cobra"
)

// Register wires the protect command tree onto root.
func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "protect",
		Short: "Configure AI coding agent protections on this machine",
		Long:  "Install, remove, or check the status of SafeDep protections for AI coding agents (MCP server, hooks, and more).",
	}
	mcp.Register(parent, a)
	root.AddCommand(parent)
}
