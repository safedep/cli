package project

import (
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "project",
		Short: "Work with SafeDep projects",
		Long: "Work with SafeDep projects, including syncing them from linked source code hosts " +
			"and scans performed by SafeDep Cloud-hosted scanners.",
	}
	scan := &cobra.Command{
		Use:   "scan",
		Short: "Manage project scans performed by SafeDep Cloud-hosted scanners",
		Long:  "Manage scans of SafeDep projects performed by SafeDep Cloud-hosted scanners.",
	}
	scan.AddCommand(createCmd(a))
	scan.AddCommand(listCmd(a))
	parent.AddCommand(scan)
	parent.AddCommand(syncCmd(a))
	root.AddCommand(parent)
}
