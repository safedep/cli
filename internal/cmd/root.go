package cmd

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	outputFlag  string
	noColorFlag bool
	verboseFlag bool
)

// NewRootCommand creates the root cobra command. The provided App is
// mutated in PersistentPreRunE (output format resolved from flags) before
// any RunE fires, so domain commands can safely use it at call time.
func NewRootCommand(a *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:           "safedep",
		Short:         "The SafeDep Platform CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVarP(&outputFlag, "output", "o", "", "output format: table, plain, json")
	root.PersistentFlags().BoolVar(&noColorFlag, "no-color", false, "disable color output")
	root.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "verbose output")

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		format, err := output.ParseFormat(outputFlag)
		if err != nil {
			return err
		}
		a.Output = output.New(format)
		return nil
	}

	return root
}
