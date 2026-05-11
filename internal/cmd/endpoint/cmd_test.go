package endpoint

import (
	"testing"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister_buildsEndpointTree(t *testing.T) {
	a := app.New(&config.Config{})
	t.Cleanup(a.Close)

	root := &cobra.Command{Use: "safedep"}
	Register(root, a)

	parent, _, err := root.Find([]string{"endpoint"})
	require.NoError(t, err)
	require.NotNil(t, parent)
	assert.NotEmpty(t, parent.Short)
	assert.NotEmpty(t, parent.Long)

	for _, leaf := range [][]string{
		{"endpoint", "status"},
		{"endpoint", "list"},
		{"endpoint", "show"},
		{"endpoint", "activity", "list"},
		{"endpoint", "inventory", "list"},
	} {
		t.Run(joinPath(leaf), func(t *testing.T) {
			cmd, _, err := root.Find(leaf)
			require.NoError(t, err)
			require.NotNil(t, cmd)
			assert.NotEmpty(t, cmd.Short)
			assert.NotEmpty(t, cmd.Long)
		})
	}
}

func joinPath(p []string) string {
	out := ""
	for i, s := range p {
		if i > 0 {
			out += "/"
		}
		out += s
	}
	return out
}
