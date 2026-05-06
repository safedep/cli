package jfrog

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register attaches the `jfrog` sub-command (and its verbs) under the
// supplied parent. Called by the `integration` package during root command
// assembly; not invoked directly by main.
func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "jfrog",
		Short: "JFrog XRay integration commands",
		Long:  "Commands for integrating SafeDep threat intelligence with JFrog XRay.",
	}

	cmd.AddCommand(runCmd(a))
	parent.AddCommand(cmd)
}
