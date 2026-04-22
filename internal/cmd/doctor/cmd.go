package doctor

import (
	"fmt"

	"github.com/safedep/cli/internal/app"
	doctordomain "github.com/safedep/cli/internal/domain/doctor"
	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	root.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Diagnose auth, protect, and config state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, a)
		},
	})
}

func runDoctor(cmd *cobra.Command, a *app.App) error {
	_, authErr := a.CredentialResolver().Resolve()

	checker := &doctordomain.Checker{}
	result := checker.Check(cmd.Context(), doctordomain.CheckInput{
		Authenticated: authErr == nil,
		Tenant:        a.Config.Tenant,
		MCPAdapters:   adapter.All(),
		OptionalTools: []string{"vet", "gryph"},
	})

	if err := a.Output.Print(result); err != nil {
		return err
	}

	if !result.AllOK {
		return fmt.Errorf("one or more checks failed — see warnings above")
	}

	return nil
}
