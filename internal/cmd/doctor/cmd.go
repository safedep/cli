package doctor

import (
	"fmt"
	"os/exec"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	root.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Diagnose auth, protect, and config state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, a)
		},
	})
}

func runDoctor(cmd *cobra.Command, a *app.App) error {
	ctx := cmd.Context()
	ok := true

	// Auth check
	if _, err := a.CredResolver.Resolve(); err != nil {
		a.Output.Warning("auth: not authenticated — run `safedep auth login`")
		ok = false
	} else {
		a.Output.Success("auth: credentials found")
	}

	// Config check
	if a.Config.Tenant == "" {
		a.Output.Warning("config: tenant not set in %s", "~/.config/safedep/config.toml")
		ok = false
	} else {
		a.Output.Success("config: tenant = %s", a.Config.Tenant)
	}

	// MCP adapters
	adapters := adapter.All()
	for _, ad := range adapters {
		result, err := ad.Detect(ctx)
		if err != nil || !result.Found {
			a.Output.Info("protect/mcp: %s not detected", ad.DisplayName())
			continue
		}

		st, err := ad.Status(ctx)
		if err != nil {
			a.Output.Warning("protect/mcp: %s status error: %v", ad.DisplayName(), err)
			ok = false
			continue
		}

		if !st.Installed {
			a.Output.Warning("protect/mcp: %s detected but SafeDep MCP not configured — run `safedep protect mcp install`", ad.DisplayName())
			ok = false
		} else {
			a.Output.Success("protect/mcp: %s configured (%s)", ad.DisplayName(), st.ConfigPath)
		}
	}

	// Optional tool checks (vet, gryph)
	for _, tool := range []string{"vet", "gryph"} {
		if path, err := exec.LookPath(tool); err == nil {
			a.Output.Success("tools: %s found at %s", tool, path)
		} else {
			a.Output.Info("tools: %s not found on PATH (optional)", tool)
		}
	}

	if !ok {
		return fmt.Errorf("one or more checks failed — see warnings above")
	}

	a.Output.Success("All checks passed.")
	return nil
}
