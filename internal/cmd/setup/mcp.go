package setup

import (
	"github.com/charmbracelet/huh"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/protect/mcp"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
)

func mcpCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Authenticate and configure SafeDep MCP in your AI IDEs (one command)",
		Long: `setup mcp is a shortcut for:
  1. safedep auth login
  2. safedep protect mcp install

Run this once on a new machine to get protected.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupMCP(cmd, a)
		},
	}
}

func runSetupMCP(cmd *cobra.Command, a *app.App) error {
	a.Output.Info("Welcome to SafeDep. Let's get you protected.")
	a.Output.Info("")

	// Step 1: authenticate
	var apiKey, tenant string

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Authenticate").
				Description("Create an API key at app.safedep.io → Settings → API Keys"),
			huh.NewInput().
				Title("API Key").
				EchoMode(huh.EchoModePassword).
				Value(&apiKey),
			huh.NewInput().
				Title("Tenant domain").
				Placeholder("acme-corp.safedep.io").
				Value(&tenant),
		),
	).Run(); err != nil {
		return err
	}

	if err := a.CredStore.SaveAPIKeyCredential(apiKey, tenant); err != nil {
		return err
	}

	a.Config.Tenant = tenant
	if err := config.Save(a.Config); err != nil {
		return err
	}

	a.Output.Success("Authenticated. Tenant: %s", tenant)
	a.Output.Info("")

	// Step 2: install MCP config
	a.Output.Info("Scanning for AI IDEs...")

	if err := mcp.RunInstall(cmd.Context(), a); err != nil {
		return err
	}

	a.Output.Info("")
	a.Output.Success("You're protected.")
	a.Output.Info("To verify: ask your AI IDE to install `safedep-test-pkg` from npm — SafeDep will block it.")
	return nil
}
