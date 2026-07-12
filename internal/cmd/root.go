package cmd

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/tui"
	"github.com/safedep/cli/internal/version"
	"github.com/safedep/dry/tui/banner"
	tuioutput "github.com/safedep/dry/tui/output"
	"github.com/spf13/cobra"
)

const bannerArt = ` ___  __ _ / _| ___  __| | ___ _ __
/ __|/ _' | |_ / _ \/ _' |/ _ \ '_ \
\__ \ (_| |  _|  __/ (_| |  __/ |_) |
|___/\__,_|_|  \___|\__,_|\___| .__/
                              |_|`

var (
	outputFlag                   string
	profileFlag                  string
	insecureKeychainFallbackFlag bool
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
	root.PersistentFlags().BoolVar(&insecureKeychainFallbackFlag, "insecure-keychain-fallback", false, "store credentials in a plaintext file when no OS keychain is available (insecure)")

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		mode, err := tui.ParseMode(outputFlag)
		if err != nil {
			return err
		}
		a.Output = tui.NewPrinter(mode)
		a.SetProfile(profileFlag)
		a.SetInsecureKeychainFallback(insecureKeychainFallbackFlag)
		return nil
	}

	// Rich-only banner on the root help so interactive first contact has
	// identity. Plain and agent modes keep cobra's unmodified help output.
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == root && tuioutput.CurrentMode() == tuioutput.Rich {
			banner.Banner{
				Art:     bannerArt,
				Name:    "cli",
				Version: version.Version,
				Tagline: "The unified CLI for the SafeDep platform",
			}.Print()
		}
		defaultHelp(cmd, args)
	})

	return root
}
