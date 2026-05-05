// internal/cmd/integration/cmd.go
package integration

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/integration/jfrog"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "integration",
		Short: "Manage SafeDep integrations",
		Long:  "Commands for integrating SafeDep threat intelligence with third-party tools.",
	}

	jfrog.Register(cmd, a)
	root.AddCommand(cmd)
}
