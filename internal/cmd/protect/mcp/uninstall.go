package mcp

import (
	"context"

	"github.com/safedep/cli/internal/app"
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
	adapters := adapter.All()
	removed := 0

	for _, ad := range adapters {
		result, err := ad.Detect(ctx)
		if err != nil || !result.Found {
			continue
		}

		if err := ad.Uninstall(ctx); err != nil {
			a.Output.Warning("%s: %v", ad.DisplayName(), err)
			continue
		}

		a.Output.Success("%s: SafeDep MCP entry removed", ad.DisplayName())
		removed++
	}

	if removed == 0 {
		a.Output.Info("No IDEs with SafeDep MCP config found.")
	}

	return nil
}
