package query

import (
	"testing"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister_buildsQueryTree(t *testing.T) {
	a := app.New(&config.Config{})
	t.Cleanup(a.Close)

	root := &cobra.Command{Use: "safedep"}
	Register(root, a)

	queryParent, _, err := root.Find([]string{"query"})
	require.NoError(t, err)
	require.NotNil(t, queryParent)
	assert.NotEmpty(t, queryParent.Short)
	assert.NotEmpty(t, queryParent.Long)

	t.Run("exec", func(t *testing.T) {
		leaf, _, err := root.Find([]string{"query", "exec"})
		require.NoError(t, err)
		require.NotNil(t, leaf)
		assert.NotEmpty(t, leaf.Short)
		assert.NotEmpty(t, leaf.Long)
		assert.NotNil(t, leaf.Flags().Lookup("sql"))
		assert.NotNil(t, leaf.Flags().Lookup("sql-file"))
		assert.NotNil(t, leaf.Flags().Lookup("limit"))
	})

	t.Run("schema parent", func(t *testing.T) {
		mid, _, err := root.Find([]string{"query", "schema"})
		require.NoError(t, err)
		require.NotNil(t, mid)
		assert.NotEmpty(t, mid.Short)
		assert.NotEmpty(t, mid.Long)
	})

	t.Run("schema get", func(t *testing.T) {
		leaf, _, err := root.Find([]string{"query", "schema", "get"})
		require.NoError(t, err)
		require.NotNil(t, leaf)
		assert.NotEmpty(t, leaf.Short)
		assert.NotEmpty(t, leaf.Long)
	})
}
