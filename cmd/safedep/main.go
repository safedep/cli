package main

import (
	"fmt"
	"os"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd"
	"github.com/safedep/cli/internal/cmd/auth"
	"github.com/safedep/cli/internal/cmd/version"
	"github.com/safedep/cli/internal/config"
	clitheme "github.com/safedep/cli/internal/theme"
	drytheme "github.com/safedep/dry/tui/theme"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	drytheme.SetDefault(clitheme.CLI())

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	a := app.New(cfg)
	defer a.Close()

	root := cmd.NewRootCommand(a)
	auth.Register(root, a)
	version.Register(root, a)

	return root.Execute()
}
