package auth

import (
	"errors"

	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func loginCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate with SafeDep Cloud",
		Long:  "Authenticate with SafeDep Cloud and store credentials in the keychain under the active profile.",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = a
			return errors.New("auth login: not yet implemented")
		},
	}
}
