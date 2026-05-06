package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	dataDirEnvOverride = "SAFEDEP_DATA_DIR"
	xdgDataHomeEnv     = "XDG_DATA_HOME"

	// dataHomeRelative mirrors configHomeRelative: SafeDep-namespaced
	// subdir under the user's platform data location.
	dataHomeRelative = "safedep/" + configToolName

	dbFileName = "state.db"
)

// StorageConfig captures persistent-state configuration. Zero value
// plus DefaultStorageConfig produces a usable config.
type StorageConfig struct {
	// Retention overrides keyed by storage primitive name (e.g. "kv").
	// Zero duration means "no time-based cleanup, only TTL."
	Retention map[string]time.Duration `mapstructure:"retention"`
}

// DefaultStorageConfig returns the baseline storage configuration. No
// per-primitive defaults are set here today: descriptor-level defaults
// inside internal/storage are authoritative until users opt into an
// override.
func DefaultStorageConfig() StorageConfig {
	return StorageConfig{
		Retention: map[string]time.Duration{},
	}
}

// DataDir returns the directory where the CLI stores durable state
// (sqlite DB, future caches that survive across runs). Internal
// resolution order:
//
//  1. SAFEDEP_DATA_DIR joined with this tool's name.
//  2. XDG_DATA_HOME joined with "safedep/cli".
//  3. $HOME/.local/share/safedep/cli when that path already exists.
//  4. Platform default joined with "safedep/cli":
//     Linux: ~/.local/share, macOS: ~/Library/Application Support,
//     Windows: %LOCALAPPDATA%.
func DataDir() (string, error) {
	if v := os.Getenv(dataDirEnvOverride); v != "" {
		return filepath.Join(v, configToolName), nil
	}

	if v := os.Getenv(xdgDataHomeEnv); v != "" {
		return filepath.Join(v, dataHomeRelative), nil
	}

	if home, err := os.UserHomeDir(); err == nil {
		xdgDefault := filepath.Join(home, ".local", "share", dataHomeRelative)
		if info, err := os.Stat(xdgDefault); err == nil && info.IsDir() {
			return xdgDefault, nil
		}
	}

	base, err := userDataDir()
	if err != nil {
		return "", fmt.Errorf("config: user data dir: %w", err)
	}
	return filepath.Join(base, dataHomeRelative), nil
}

// DBPath returns the absolute path to the CLI's sqlite database.
func DBPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dbFileName), nil
}
