// Package version registers the `safedep version` command. version is an
// intentional exception to the noun-verb shape rule per DEVGUIDE: every
// CLI in the ecosystem (kubectl, gh, git, vet, pmg) responds to a bare
// `version` and the convention is older than ours.
//
// Output bypasses internal/tui because version is the one command whose
// output is parsed by humans and scripts equally and must not carry
// dry/tui's [INFO] prefix or any decoration.
package version

import (
	"fmt"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/version"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, _ *app.App) {
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print CLI version",
		Long:  "Print the version, commit, and build metadata of this CLI.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"Version:   %s\nCommitSHA: %s\n",
				version.Version, version.Commit)
			return err
		},
	})
}
