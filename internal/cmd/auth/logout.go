package auth

import (
	"github.com/safedep/cli/internal/app"
	authdomain "github.com/safedep/cli/internal/domain/auth"
	"github.com/spf13/cobra"
)

func logoutCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			saver := &authdomain.Saver{Store: a.CredentialStore()}
			if err := saver.Clear(); err != nil {
				return err
			}
			a.Output.Success("Logged out. Credentials removed.")
			return nil
		},
	}
}
