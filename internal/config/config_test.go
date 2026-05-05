package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDir(t *testing.T) {
	t.Run("SAFEDEP_CONFIG_DIR is the shared root with tool subdir appended", func(t *testing.T) {
		t.Setenv(configDirEnvOverride, "/opt/safedep")
		t.Setenv(xdgConfigHomeEnv, "/xdg/should/not/be/used")
		t.Setenv("HOME", t.TempDir())

		got, err := dir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("/opt/safedep", configToolName), got,
			"override is the shared root; cli must occupy its own subdir under it")
	})

	t.Run("XDG_CONFIG_HOME used when no override", func(t *testing.T) {
		t.Setenv(configDirEnvOverride, "")
		t.Setenv(xdgConfigHomeEnv, "/xdg")
		t.Setenv("HOME", t.TempDir())

		got, err := dir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("/xdg", configHomeRelative), got)
	})

	t.Run("existing ~/.config/safedep/cli is preferred over platform default", func(t *testing.T) {
		home := t.TempDir()
		existing := filepath.Join(home, ".config", configHomeRelative)
		require.NoError(t, os.MkdirAll(existing, 0o700))

		t.Setenv(configDirEnvOverride, "")
		t.Setenv(xdgConfigHomeEnv, "")
		t.Setenv("HOME", home)

		got, err := dir()
		require.NoError(t, err)
		assert.Equal(t, existing, got)
	})

	t.Run("platform default when nothing else applies", func(t *testing.T) {
		home := t.TempDir() // empty: ~/.config/safedep/cli does not exist
		t.Setenv(configDirEnvOverride, "")
		t.Setenv(xdgConfigHomeEnv, "")
		t.Setenv("HOME", home)

		base, err := os.UserConfigDir()
		require.NoError(t, err, "platform must support UserConfigDir for this test")
		want := filepath.Join(base, configHomeRelative)

		got, err := dir()
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("non-override branches end with safedep/cli", func(t *testing.T) {
		t.Setenv(configDirEnvOverride, "")
		t.Setenv(xdgConfigHomeEnv, "/xdg")
		t.Setenv("HOME", t.TempDir())

		got, err := dir()
		require.NoError(t, err)
		assert.True(t, filepath.Base(got) == "cli" && filepath.Base(filepath.Dir(got)) == "safedep",
			"expected path to end with safedep/cli, got %q", got)
	})

	t.Run("override branch ends with cli but no safedep prefix (root is already safedep-namespaced)", func(t *testing.T) {
		t.Setenv(configDirEnvOverride, "/some/root")
		t.Setenv(xdgConfigHomeEnv, "")
		t.Setenv("HOME", t.TempDir())

		got, err := dir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("/some/root", "cli"), got)
		assert.NotEqual(t, "safedep", filepath.Base(filepath.Dir(got)),
			"override is the safedep namespace; we must not double-prefix")
	})
}
