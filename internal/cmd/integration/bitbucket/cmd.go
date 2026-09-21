// Package bitbucket manages the Bitbucket Cloud integration: workspace link
// codes for the SafeDep Forge app, linked workspaces, and the repository
// scan allowlist.
package bitbucket

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register attaches the `bitbucket` sub-noun and its commands to the
// `integration` parent command.
func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "bitbucket",
		Short: "Manage the Bitbucket Cloud integration",
		Long: "Commands for linking Bitbucket workspaces to the active SafeDep tenant " +
			"through the SafeDep Forge app and for selecting which repositories SafeDep scans.",
	}

	link := &cobra.Command{
		Use:   "link",
		Short: "Manage Bitbucket workspace links",
		Long: "Commands for issuing workspace link codes and listing the Bitbucket " +
			"workspaces linked to the active tenant.",
	}
	link.AddCommand(linkCreateCmd(a))
	link.AddCommand(linkListCmd(a))

	repository := &cobra.Command{
		Use:   "repository",
		Short: "Inspect repositories of a linked Bitbucket workspace",
		Long: "Commands for listing the repositories of a linked Bitbucket workspace " +
			"with their scan state.",
	}
	repository.AddCommand(repositoryListCmd(a))

	allowlist := &cobra.Command{
		Use:   "allowlist",
		Short: "Manage the Bitbucket repository scan allowlist",
		Long: "Commands for changing the scan scope of a linked Bitbucket workspace " +
			"and the repositories on its scan allowlist.",
	}
	allowlist.AddCommand(allowlistUpdateCmd(a))

	cmd.AddCommand(link)
	cmd.AddCommand(repository)
	cmd.AddCommand(allowlist)
	parent.AddCommand(cmd)
}
