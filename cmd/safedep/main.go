package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd"
	"github.com/safedep/cli/internal/config"
	"github.com/safedep/cli/internal/tui"
	drytheme "github.com/safedep/dry/tui/theme"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errAuthLoginRequired = errors.New("not authenticated: run `safedep auth login`")

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", normalizeRunError(err))
		os.Exit(1)
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
	if usefulErr, ok := usefulerror.AsUsefulError(err); ok {
		return formatUsefulError(usefulErr)
	}

	return err
}

func formatUsefulError(err usefulerror.UsefulError) error {
	lines := []string{err.HumanError()}
	if code := err.Code(); code != "" && code != "unknown" {
		lines = append(lines, "Code: "+code)
	}
	if help := err.Help(); help != "" && help != "No additional help is available for this error." {
		lines = append(lines, "Help: "+help)
	}
	if referenceURL := err.ReferenceURL(); referenceURL != "" {
		lines = append(lines, "More: "+referenceURL)
	}
	return errors.New(strings.Join(lines, "\n"))
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
