package mcp

import (
	"context"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/spf13/cobra"
)

func installCmd(a *app.App) *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Inject SafeDep MCP server config into detected AI IDEs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd.Context(), a, global)
		},
	}

	cmd.Flags().BoolVar(&global, "global", true, "write to global IDE config (default: true)")
	return cmd
}

// RunInstall is exported for use by setup mcp.
func RunInstall(ctx context.Context, a *app.App) error {
	return runInstall(ctx, a, true)
}

func runInstall(ctx context.Context, a *app.App, global bool) error {
	creds, err := a.CredResolver.Resolve()
	if err != nil {
		return err
	}

	apiKey, err := creds.GetAPIKey()
	if err != nil {
		return err
	}

	tenantDomain, err := creds.GetTenantDomain()
	if err != nil {
		return err
	}

	mcpCreds := adapter.MCPCredentials{
		APIKey:   apiKey,
		TenantID: tenantDomain,
	}

	adapters := adapter.All()
	installed := 0

	for _, ad := range adapters {
		result, err := ad.Detect(ctx)
		if err != nil || !result.Found {
			continue
		}

		a.Output.Info("Configuring %s...", ad.DisplayName())

		if err := ad.Install(ctx, mcpCreds); err != nil {
			a.Output.Warning("%s: %v", ad.DisplayName(), err)
			continue
		}

		a.Output.Success("%s configured", ad.DisplayName())
		installed++
	}

	if installed == 0 {
		a.Output.Warning("No supported AI IDEs detected. Install Claude Code, Cursor, or Windsurf and retry.")
		return nil
	}

	a.Output.Success("SafeDep MCP server configured in %d IDE(s). Restart your IDE to apply.", installed)
	return nil
}
