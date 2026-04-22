package query

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	c := &cobra.Command{
		Use:   "query",
		Short: "Query cloud data (requires OAuth login)",
	}

	c.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Execute a SQL query against SafeDep Cloud",
		RunE:  func(_ *cobra.Command, _ []string) error { return stub(a) },
	})

	root.AddCommand(c)
}

func stub(a *app.App) error {
	_, err := a.RequireControlPlane()
	return err
}
