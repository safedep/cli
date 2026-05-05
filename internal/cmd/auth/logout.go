package auth

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"
)

func logoutCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove credentials for the active profile",
		Long:  "Remove the credentials stored for the active profile from the keychain.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := a.CredentialStore()
			if err != nil {
				return err
			}
			if err := store.Clear(); err != nil {
				return err
			}
			tui.Success("Cleared credentials for profile %q", a.Profile())
			return nil
		},
	}
}
