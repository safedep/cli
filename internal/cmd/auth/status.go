package auth

import (
	"errors"

	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func statusCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status for the active profile",
		Long:  "Report whether the active profile holds valid credentials and which tenant they bind to.",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = a
			return errors.New("auth status: not yet implemented")
		},
	}
}
