package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDataDir_SafeDepDataDirOverride(t *testing.T) {
	t.Setenv(dataDirEnvOverride, "/tmp/sd-data")
	got, err := DataDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/tmp/sd-data", "cli"), got)
}

func TestDataDir_XDGDataHome(t *testing.T) {
	t.Setenv(dataDirEnvOverride, "")
	t.Setenv(xdgDataHomeEnv, "/tmp/xdg")
	// Ensure no pre-existing XDG dir on disk wins precedence.
	t.Setenv("HOME", t.TempDir())
	got, err := DataDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/tmp/xdg", "safedep", "cli"), got)
}

func TestDataDir_PlatformDefault(t *testing.T) {
	t.Setenv(dataDirEnvOverride, "")
	t.Setenv(xdgDataHomeEnv, "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := DataDir()
	require.NoError(t, err)

	switch runtime.GOOS {
	case "darwin":
		require.Equal(t, filepath.Join(home, "Library", "Application Support", "safedep", "cli"), got)
	case "linux":
		require.Equal(t, filepath.Join(home, ".local", "share", "safedep", "cli"), got)
	}
}

func TestDataDir_PreferExistingXDGDir(t *testing.T) {
	t.Setenv(dataDirEnvOverride, "")
	t.Setenv(xdgDataHomeEnv, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	xdgDefault := filepath.Join(home, ".local", "share", "safedep", "cli")
	require.NoError(t, os.MkdirAll(xdgDefault, 0o700))
	got, err := DataDir()
	require.NoError(t, err)
	require.Equal(t, xdgDefault, got)
}

func TestDBPath_JoinsStateDB(t *testing.T) {
	t.Setenv(dataDirEnvOverride, "/tmp/sd-data")
	got, err := DBPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/tmp/sd-data", "cli", "state.db"), got)
}

func TestDefaultStorageConfig_HasNonNilRetention(t *testing.T) {
	cfg := DefaultStorageConfig()
	require.NotNil(t, cfg.Retention)
	require.Empty(t, cfg.Retention)
}
