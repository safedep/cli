package main

import (
	"fmt"
	"os"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd"
	"github.com/safedep/cli/internal/cmd/apikey"
	"github.com/safedep/cli/internal/cmd/auth"
	"github.com/safedep/cli/internal/cmd/doctor"
	"github.com/safedep/cli/internal/cmd/exclusion"
	"github.com/safedep/cli/internal/cmd/project"
	"github.com/safedep/cli/internal/cmd/protect"
	"github.com/safedep/cli/internal/cmd/query"
	"github.com/safedep/cli/internal/cmd/scan"
	"github.com/safedep/cli/internal/cmd/setup"
	"github.com/safedep/cli/internal/cmd/tenant"
	"github.com/safedep/cli/internal/config"
	"github.com/safedep/cli/internal/output"
	clitui "github.com/safedep/cli/internal/tui"
	"github.com/safedep/cli/internal/version"
	"github.com/safedep/dry/tui/theme"
	"github.com/spf13/cobra"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	theme.SetDefault(clitui.CLITheme())

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	a, err := app.New(cfg, output.DefaultFormat())
	if err != nil {
		return fmt.Errorf("failed to initialise: %w", err)
	}
	defer a.Close()

	root := cmd.NewRootCommand(a)

	// Phase 1: fully implemented
	auth.Register(root, a)
	protect.Register(root, a)
	setup.Register(root, a)
	doctor.Register(root, a)

	// Phase 2 stubs: visible in --help, return clear error on use
	scan.Register(root, a)
	query.Register(root, a)
	project.Register(root, a)
	apikey.Register(root, a)
	tenant.Register(root, a)
	exclusion.Register(root, a)

	root.AddCommand(versionCmd())

	return root.Execute()
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(version.String())
		},
	}
}
