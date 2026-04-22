package mcp

import (
	"context"

	"github.com/safedep/cli/internal/app"
	mcpdomain "github.com/safedep/cli/internal/domain/protect/mcp"
	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/spf13/cobra"
)

func uninstallCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove SafeDep MCP server config from AI IDEs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(cmd.Context(), a)
		},
	}
}

func runUninstall(ctx context.Context, a *app.App) error {
	prov := &mcpdomain.Provisioner{}
	result, err := prov.Deprovision(ctx, adapter.All())
	if err != nil {
		return err
	}

	for _, w := range result.Warnings {
		a.Output.Warning("%s", w)
	}

	if result.Removed == 0 {
		a.Output.Info("No IDEs with SafeDep MCP config found.")
		return nil
	}

	a.Output.Success("SafeDep MCP entry removed from %d IDE(s).", result.Removed)
	return nil
}
