package scan

import (
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "scan",
		Short: "Manage project scans",
		Long:  "Submit and manage on-demand project scans in SafeDep Cloud.",
	}
	parent.AddCommand(createCmd(a))
	root.AddCommand(parent)
}
