package setup

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Guided setup shortcuts",
	}

	cmd.AddCommand(mcpCmd(a))
	root.AddCommand(cmd)
}
