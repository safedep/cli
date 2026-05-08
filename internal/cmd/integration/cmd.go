// Package integration is the umbrella domain for SafeDep's third-party
// integrations. Each sub-noun (jfrog, slack, ...) lives in its own package
// and self-registers under the `integration` parent command.
package integration

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/integration/jfrog"
	"github.com/spf13/cobra"
)

// Register attaches the `integration` parent command and all of its
// sub-commands to the supplied root.
func Register(root *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "integration",
		Short: "Manage SafeDep integrations",
		Long:  "Commands for integrating SafeDep threat intelligence with third-party tools.",
	}

	jfrog.Register(cmd, a)
	root.AddCommand(cmd)
}
