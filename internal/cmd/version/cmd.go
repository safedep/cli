// Package version registers the `safedep version` domain.
package version

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "version",
		Short: "Inspect CLI version information",
		Long:  "Inspect the version, commit, and build metadata of this CLI.",
	}
	parent.AddCommand(showCmd(a))
	root.AddCommand(parent)
}
