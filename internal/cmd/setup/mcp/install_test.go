package mcp

import (
	"testing"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMCPParent(t *testing.T) *cobra.Command {
	t.Helper()
	a := app.New(&config.Config{})
	t.Cleanup(a.Close)

	parent := &cobra.Command{Use: "setup"}
	Register(parent, a)
	return parent
}

func findCommand(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func TestInstallCmd_Registration(t *testing.T) {
	parent := newMCPParent(t)

	mcpCmd := findCommand(parent, "mcp")
	require.NotNil(t, mcpCmd, "mcp sub-command must be registered")

	installCmd := findCommand(mcpCmd, "install")
	require.NotNil(t, installCmd, "install sub-command must be registered under mcp")

	assert.Equal(t, "install", installCmd.Name())
	assert.NotEmpty(t, installCmd.Short)
	assert.NotEmpty(t, installCmd.Long)
}

func TestInstallCmd_Flags(t *testing.T) {
	parent := newMCPParent(t)
	mcpCmd := findCommand(parent, "mcp")
	require.NotNil(t, mcpCmd)
	installCmd := findCommand(mcpCmd, "install")
	require.NotNil(t, installCmd)

	mcpURLFlag := installCmd.Flags().Lookup("mcp-url")
	require.NotNil(t, mcpURLFlag, "--mcp-url flag must exist")
	assert.Equal(t, defaultMCPServerURL, mcpURLFlag.DefValue)

	workspaceFlag := installCmd.Flags().Lookup("workspace")
	require.NotNil(t, workspaceFlag, "--workspace flag must exist")
	assert.Equal(t, "", workspaceFlag.DefValue)

	forceFlag := installCmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag, "--force flag must exist")
	assert.Equal(t, "false", forceFlag.DefValue)
}
