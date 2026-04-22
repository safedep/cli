package auth

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication and credentials",
	}

	cmd.AddCommand(loginCmd(a))
	cmd.AddCommand(logoutCmd(a))
	cmd.AddCommand(statusCmd(a))

	root.AddCommand(cmd)
}
