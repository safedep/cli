package main

import (
	"errors"
	"fmt"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd"
	"github.com/safedep/cli/internal/config"
	"github.com/safedep/cli/internal/tui"
	tuierrors "github.com/safedep/dry/tui/errors"
	drytheme "github.com/safedep/dry/tui/theme"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errAuthLoginRequired = errors.New("not authenticated: run `safedep auth login`")

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
		return errAuthLoginRequired
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
