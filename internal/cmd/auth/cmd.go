// Package auth registers the `safedep auth` domain. Verbs in this package
// are placeholders during foundation bring-up; they exist so the command
// tree, output dispatch, and profile flag are exercisable end-to-end.
package auth

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication and credentials",
		Long:  "Authenticate with SafeDep Cloud and manage credential profiles.",
	}

	parent.AddCommand(loginCmd(a))
	parent.AddCommand(logoutCmd(a))
	parent.AddCommand(statusCmd(a))
	parent.AddCommand(profileCmd(a))

	root.AddCommand(parent)
}
