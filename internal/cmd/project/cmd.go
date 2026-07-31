package project

import (
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "project",
		Short: "Work with SafeDep projects",
		Long:  "Work with SafeDep projects, including scans performed by SafeDep Cloud-hosted scanners.",
	}
	scan := &cobra.Command{
		Use:   "scan",
		Short: "Manage project scans performed by SafeDep Cloud-hosted scanners",
		Long:  "Manage scans of SafeDep projects performed by SafeDep Cloud-hosted scanners.",
	}
	scan.AddCommand(createCmd(a))
	parent.AddCommand(scan)
	root.AddCommand(parent)
}
