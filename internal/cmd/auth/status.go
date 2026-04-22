package auth

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func statusCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication state",
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, err := a.CredentialResolver().Resolve()
			if err != nil {
				a.Output.Warning("Not authenticated. Run `safedep auth login`.")
				return nil
			}

			tenant, _ := creds.GetTenantDomain()
			a.Output.Success("Authenticated")
			a.Output.Info("Tenant: %s", tenant)
			return nil
		},
	}
}
