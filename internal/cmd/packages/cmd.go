// Package packages registers the `safedep package` domain. Today it hosts
// on-demand package scanning (`package scan`) backed by the control-plane
// PackageScanService. The domain noun is deliberately generic: a package is
// any external software component in a supported ecosystem (OSS libraries,
// IDE/editor extensions, and more), not only OSS libraries.
package packages

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register wires the package command tree onto root.
func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "package",
		Short: "Work with software packages",
		Long:  "Commands for working with software packages across supported ecosystems.",
	}

	scan := &cobra.Command{
		Use:   "scan",
		Short: "On-demand package scanning",
		Long:  "Submit packages for on-demand malware scanning via SafeDep Cloud, and inspect the results.",
	}
	scan.AddCommand(runCmd(a))
	scan.AddCommand(getCmd(a))
	scan.AddCommand(listCmd(a))
	scan.AddCommand(showCmd(a))

	parent.AddCommand(scan)
	root.AddCommand(parent)
}
