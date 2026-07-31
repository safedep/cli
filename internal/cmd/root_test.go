package cmd

import (
	"testing"

	"github.com/safedep/dry/tui/output"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
)

func TestRootCommandVerboseFlag(t *testing.T) {
	output.SetVerbosity(output.Normal)
	t.Cleanup(func() { output.SetVerbosity(output.Normal) })

	a := app.New(&config.Config{})
	root := NewRootCommand(a)
	root.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(_ *cobra.Command, _ []string) error {
			assert.Equal(t, output.Verbose, output.CurrentVerbosity())
			return nil
		},
	})
	root.SetArgs([]string{"probe", "--verbose"})

	require.NoError(t, root.Execute())
}
