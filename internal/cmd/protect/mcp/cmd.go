package mcp

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register adds the `mcp` sub-command tree to parent (the `setup` command).
func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage SafeDep MCP server configuration for AI agents",
		Long: "Install or remove the SafeDep MCP server from the configuration files of AI " +
			"coding agents (Claude Code, Cursor, Gemini CLI, and others) detected on this machine.",
	}
	cmd.AddCommand(installCmd(a))
	cmd.AddCommand(uninstallCmd(a))
	cmd.AddCommand(statusCmd(a))
	parent.AddCommand(cmd)
}
