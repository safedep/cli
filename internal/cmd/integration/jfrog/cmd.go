// internal/cmd/integration/jfrog/cmd.go
package jfrog

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "jfrog",
		Short: "JFrog XRay integration commands",
		Long:  "Commands for integrating SafeDep threat intelligence with JFrog XRay.",
	}

	cmd.AddCommand(runCmd(a))
	parent.AddCommand(cmd)
}
