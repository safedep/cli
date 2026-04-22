package protect

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/protect/mcp"
	"github.com/spf13/cobra"
)

func statusCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show protection status across all mechanisms",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcp.PrintStatus(cmd.Context(), a)
		},
	}
}
