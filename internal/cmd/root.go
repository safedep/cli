package cmd

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/tui"
	"github.com/spf13/cobra"
)

var (
	outputFlag  string
	profileFlag string
)

// NewRootCommand creates the root cobra command. Persistent flags are
// resolved in PersistentPreRunE before any RunE fires, so domain commands
// can read App.Output and the resolved profile at call time.
func NewRootCommand(a *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:           "safedep",
		Short:         "The SafeDep Platform CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVarP(&outputFlag, "output", "o", "", "output mode: table, plain, json (auto-detected when empty)")
	root.PersistentFlags().StringVar(&profileFlag, "profile", "", "credential profile (overrides SAFEDEP_PROFILE; defaults to \"default\")")

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		mode, err := tui.ParseMode(outputFlag)
		if err != nil {
			return err
		}
		a.Output = tui.NewPrinter(mode)
		a.SetProfile(profileFlag)
		return nil
	}

	return root
}
