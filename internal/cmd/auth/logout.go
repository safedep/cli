package auth

import (
	"errors"

	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func logoutCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove credentials for the active profile",
		Long:  "Remove the credentials stored for the active profile from the keychain.",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = a
			return errors.New("auth logout: not yet implemented")
		},
	}
}
