package auth

import (
	"fmt"

	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// tokenCmd prints the active profile's OAuth access token to stdout. It is the
// seam for external tools and scripts that call the SafeDep control plane
// directly (for example a standalone poller), so they do not each need to
// reimplement login and token refresh.
func tokenCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print the OAuth access token for the active profile",
		Long: `Print the OAuth access token for the active SafeDep profile to stdout.

Requires a prior OAuth login (see 'safedep auth login').`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := a.ControlPlaneToken()
			if err != nil {
				return err
			}
			// Write the raw token to stdout so `$(safedep auth token)` captures it.
			_, err = fmt.Fprintln(cmd.OutOrStdout(), token)
			return err
		},
	}
}
