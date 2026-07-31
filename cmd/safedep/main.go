package main

import (
	"fmt"

	tuierrors "github.com/safedep/dry/tui/errors"
	drytheme "github.com/safedep/dry/tui/theme"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/app"
	cliauth "github.com/safedep/cli/internal/auth"
	"github.com/safedep/cli/internal/cmd"
	"github.com/safedep/cli/internal/config"
	"github.com/safedep/cli/internal/tui"
)

func main() {
	if err := run(); err != nil {
		tuierrors.ErrorExit(normalizeRunError(err))
	}
}

func normalizeRunError(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if ok && st.Code() == codes.Unauthenticated {
		return cliauth.LoginRequiredError(err)
	}
	return err
}

func run() error {
	drytheme.SetDefault(tui.CLITheme())

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	a := app.New(cfg)
	defer a.Close()

	return cmd.NewSafedep(a).Execute()
}
