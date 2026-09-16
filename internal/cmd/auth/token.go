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

The token is refreshed if it has expired, so what prints is always usable. Only
the access token is printed, never the refresh token. Use it to authenticate an
external tool or script against the SafeDep control plane:

    export SAFEDEP_TOKEN=$(safedep auth token)

Requires a prior OAuth login (see 'safedep auth login'). An API-key login does
not produce an OAuth token. Access tokens are short-lived, so re-run this to get
a fresh one rather than caching it.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := a.ControlPlaneToken()
			if err != nil {
				return err
			}
			// Raw token on stdout so `$(safedep auth token)` captures only the
			// token. Not routed through the output printer, which is for
			// structured results and adds styling.
			_, err = fmt.Fprintln(cmd.OutOrStdout(), token)
			return err
		},
	}
}
