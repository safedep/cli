package protect

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/protect/mcp"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "protect",
		Short: "Configure AI IDEs and agents with SafeDep protection",
	}

	mcp.Register(cmd, a)
	cmd.AddCommand(statusCmd(a))

	root.AddCommand(cmd)
}
