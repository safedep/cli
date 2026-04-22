package mcp

import (
	"context"

	"github.com/safedep/cli/internal/app"
	mcpdomain "github.com/safedep/cli/internal/domain/protect/mcp"
	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/spf13/cobra"
)

func installCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Inject SafeDep MCP server config into detected AI IDEs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunInstall(cmd.Context(), a)
		},
	}
}

// RunInstall is exported for use by setup mcp.
func RunInstall(ctx context.Context, a *app.App) error {
	creds, err := a.CredentialResolver().Resolve()
	if err != nil {
		return err
	}

	prov := &mcpdomain.Provisioner{}
	result, err := prov.Provision(ctx, adapter.All(), creds)
	if err != nil {
		return err
	}

	for _, w := range result.Warnings {
		a.Output.Warning("%s", w)
	}

	if result.Installed == 0 {
		a.Output.Warning("No supported AI IDEs detected. Install Claude Code, Cursor, or Windsurf and retry.")
		return nil
	}

	a.Output.Success("SafeDep MCP server configured in %d IDE(s). Restart your IDE to apply.", result.Installed)
	return nil
}
