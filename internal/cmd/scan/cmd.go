package scan

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	c := &cobra.Command{
		Use:   "scan",
		Short: "Scan code and manifests for vulnerabilities (requires vet)",
	}

	for _, sub := range []string{"run", "history", "results"} {
		name := sub
		c.AddCommand(&cobra.Command{
			Use:  name,
			RunE: func(_ *cobra.Command, _ []string) error { return stub() },
		})
	}

	root.AddCommand(c)
}

func stub() error {
	return cmd.ErrNotImplemented("scan requires vet — available in a future release")
}
