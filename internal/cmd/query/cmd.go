// Package query registers the `safedep query` domain. It exposes the
// SafeDep Cloud SQL query service: arbitrary SQL execution and schema
// inspection. All verbs hit the control plane via App.ControlPlane().
package query

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register wires the query command tree onto root.
func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "query",
		Short: "Query SafeDep Cloud with SQL",
		Long:  "Run SQL queries against SafeDep Cloud's analytics surface and inspect the available schema.",
	}

	parent.AddCommand(execCmd(a))
	parent.AddCommand(schemaCmd(a))

	root.AddCommand(parent)
}
