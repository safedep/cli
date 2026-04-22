package mcp

import (
	"context"

	"github.com/safedep/cli/internal/app"
	mcpdomain "github.com/safedep/cli/internal/domain/protect/mcp"
	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/spf13/cobra"
)

func statusCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show MCP configuration status for all detected AI IDEs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return PrintStatus(cmd.Context(), a)
		},
	}
}

// PrintStatus is exported so protect status can call it.
func PrintStatus(ctx context.Context, a *app.App) error {
	checker := &mcpdomain.StatusChecker{}
	result, err := checker.Check(ctx, adapter.All())
	if err != nil {
		return err
	}

	if len(result.Adapters) == 0 {
		a.Output.Info("No supported AI IDEs detected.")
		return nil
	}

	return a.Output.Print(result)
}
