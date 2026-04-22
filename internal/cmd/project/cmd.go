package project

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	c := &cobra.Command{
		Use:   "project",
		Short: "Manage cloud projects (requires OAuth login)",
	}

	for _, sub := range []string{"list", "view", "delete"} {
		name := sub
		c.AddCommand(&cobra.Command{
			Use:  name,
			RunE: func(_ *cobra.Command, _ []string) error { _, err := a.RequireControlPlane(); return err },
		})
	}

	root.AddCommand(c)
}
