package auth

import (
	"testing"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister_buildsAuthTree(t *testing.T) {
	a := app.New(&config.Config{})
	root := &cobra.Command{Use: "safedep"}

	Register(root, a)

	authCmd, _, err := root.Find([]string{"auth"})
	require.NoError(t, err)
	require.NotNil(t, authCmd)
	assert.Equal(t, "auth", authCmd.Name())

	for _, verb := range []string{"login", "logout", "status"} {
		t.Run(verb, func(t *testing.T) {
			leaf, _, err := root.Find([]string{"auth", verb})
			require.NoError(t, err)
			require.NotNil(t, leaf)
			assert.NotEmpty(t, leaf.Short)
			assert.NotEmpty(t, leaf.Long)
		})
	}

	t.Run("profile list", func(t *testing.T) {
		leaf, _, err := root.Find([]string{"auth", "profile", "list"})
		require.NoError(t, err)
		require.NotNil(t, leaf)
		assert.NotEmpty(t, leaf.Short)
		assert.NotEmpty(t, leaf.Long)
	})
}
