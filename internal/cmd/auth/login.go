package auth

import (
	"github.com/charmbracelet/huh"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
)

func loginCmd(a *app.App) *cobra.Command {
	var withToken bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with SafeDep Cloud",
		RunE: func(cmd *cobra.Command, args []string) error {
			if withToken {
				return loginWithTokenFlag(a)
			}
			return loginInteractive(a)
		},
	}

	cmd.Flags().BoolVar(&withToken, "with-token", false, "read API key from stdin (for CI)")
	return cmd
}

func loginInteractive(a *app.App) error {
	var apiKey, tenant string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("API Key").
				Description("Create one at app.safedep.io → Settings → API Keys").
				EchoMode(huh.EchoModePassword).
				Value(&apiKey),
			huh.NewInput().
				Title("Tenant domain").
				Placeholder("acme-corp.safedep.io").
				Value(&tenant),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	return saveCredentials(a, apiKey, tenant)
}

func loginWithTokenFlag(a *app.App) error {
	apiKey := ""
	if err := huh.NewInput().
		Title("API Key").
		EchoMode(huh.EchoModePassword).
		Value(&apiKey).
		Run(); err != nil {
		return err
	}

	tenant := a.Config.Tenant
	if tenant == "" {
		if err := huh.NewInput().
			Title("Tenant domain").
			Placeholder("acme-corp.safedep.io").
			Value(&tenant).
			Run(); err != nil {
			return err
		}
	}

	return saveCredentials(a, apiKey, tenant)
}

func saveCredentials(a *app.App, apiKey, tenant string) error {
	if err := a.CredStore.SaveAPIKeyCredential(apiKey, tenant); err != nil {
		return err
	}

	a.Config.Tenant = tenant
	if err := config.Save(a.Config); err != nil {
		return err
	}

	a.Output.Success("Authenticated. Tenant: %s", tenant)
	return nil
}
