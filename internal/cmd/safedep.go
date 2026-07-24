package cmd

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/auth"
	"github.com/safedep/cli/internal/cmd/endpoint"
	"github.com/safedep/cli/internal/cmd/integration"
	"github.com/safedep/cli/internal/cmd/packages"
	"github.com/safedep/cli/internal/cmd/protect"
	"github.com/safedep/cli/internal/cmd/query"
	"github.com/safedep/cli/internal/cmd/setup"
	"github.com/safedep/cli/internal/cmd/subscription"
	"github.com/safedep/cli/internal/cmd/version"
	"github.com/spf13/cobra"
)

// NewSafedep assembles the full safedep command tree. main() and the
// convention tests both consume this so they walk an identical tree.
func NewSafedep(a *app.App) *cobra.Command {
	root := NewRootCommand(a)
	auth.Register(root, a)
	endpoint.Register(root, a)
	packages.Register(root, a)
	query.Register(root, a)
	integration.Register(root, a)
	protect.Register(root, a)
	setup.Register(root, a)
	subscription.Register(root, a)
	version.Register(root, a)
	return root
}
