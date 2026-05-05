package auth

import (
	"errors"

	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func profileCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "profile",
		Short: "Manage credential profiles",
		Long:  "Inspect credential profiles stored in the keychain.",
	}
	parent.AddCommand(profileListCmd(a))
	return parent
}

func profileListCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List credential profiles",
		Long:  "List the credential profiles known to this CLI.",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = a
			return errors.New("auth profile list: not yet implemented")
		},
	}
}
