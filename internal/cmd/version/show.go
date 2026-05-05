package version

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/version"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"
)

func showCmd(_ *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print CLI version",
		Long:  "Print the version, commit, and build metadata of this CLI.",
		RunE: func(cmd *cobra.Command, args []string) error {
			tui.Info("%s", version.String())
			return nil
		},
	}
}
