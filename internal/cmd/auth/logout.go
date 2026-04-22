package auth

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func logoutCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.CredStore.Clear(); err != nil {
				return err
			}
			a.Output.Success("Logged out. Credentials removed.")
			return nil
		},
	}
}
