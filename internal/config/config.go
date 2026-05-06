// Package config owns the CLI's persistent configuration. The fields are
// intentionally empty during foundation bring-up. Concrete settings will
// land alongside the first feature that needs them. Load/Save are kept so
// callers (App, future commands) don't move when fields return.
//
// The config directory follows the conventions used by other SafeDep
// tools (pmg, gryph): a SafeDep-namespaced subdir under the user's
// platform config location, with explicit env overrides and respect for
// XDG layouts on every platform.
//
// When log or cache dirs land later, they will need their own resolvers:
// logs belong under %LOCALAPPDATA% on Windows (per pmg #82), and cache
// belongs under os.UserCacheDir() on every platform (per gryph). This
// package owns config only today.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	configFileName = "config"

	// configToolName is this tool's namespace under the shared SafeDep
	// config root. pmg, gryph, and other tools should pick their own:
	// SAFEDEP_CONFIG_DIR is the parent. Each tool lives at <root>/<name>.
	configToolName = "cli"

	// configHomeRelative is used when the platform's user config dir is
	// the parent (every branch except the SAFEDEP_CONFIG_DIR override),
	// because that parent typically holds many unrelated tools and needs
	// the SafeDep brand prefix.
	configHomeRelative = "safedep/" + configToolName

	// configDirEnvOverride points at the SafeDep root (shared across the
	// CLI, pmg, gryph). Each tool joins its own configToolName onto it.
	configDirEnvOverride = "SAFEDEP_CONFIG_DIR"

	xdgConfigHomeEnv = "XDG_CONFIG_HOME"
)

// Config is the deserialised CLI config file. Add fields here. Load/Save
// pick them up automatically via viper's mapstructure binding.
type Config struct {
	Storage StorageConfig `mapstructure:"storage"`
}

// EnvVar returns the value of the named environment variable, or empty string
// if unset. Centralises os.Getenv usage per the project lint rule.
func EnvVar(key string) string {
	return os.Getenv(key)
}

// Load reads the config file into a Config. A missing file is not an
// error: it returns a zero Config so first-run commands behave like a
// fresh install. Any other read error (permission, malformed) is fatal.
func Load() (*Config, error) {
	dir, err := dir()
	if err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigName(configFileName)
	v.SetConfigType("toml")
	v.AddConfigPath(dir)

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("config: read: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}

	// Defaults for absent sections. Viper's StringToTimeDurationHookFunc
	// is required for the Retention map to decode duration strings; the
	// wiring PR adds it. Until then, an empty/missing section yields the
	// default values below.
	if cfg.Storage.Retention == nil {
		cfg.Storage = DefaultStorageConfig()
	}

	return &cfg, nil
}

// Save writes cfg to the config file, creating the directory if needed.
func Save(cfg *Config) error {
	dir, err := dir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}

	v := viper.New()
	_ = cfg // no fields yet; viper.WriteConfigAs writes an empty document.

	path := filepath.Join(dir, configFileName+".toml")
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}

	return nil
}

// dir returns the config directory. Resolution order:
//
//  1. SAFEDEP_CONFIG_DIR joined with this tool's name. The env var points
//     at the shared SafeDep root. Each tool (cli, pmg, gryph, ...)
//     occupies its own subdir under it. This keeps CI/test setups
//     consistent across the tool family.
//  2. XDG_CONFIG_HOME joined with "safedep/cli" if set. Honoured on every
//     platform, not just Linux, so users who organise XDG-style on macOS
//     or Windows get a consistent layout. (Go's os.UserConfigDir only
//     honours XDG on Linux.)
//  3. $HOME/.config/safedep/cli if that directory already exists. Lets
//     users who already have an XDG layout on macOS or Windows keep
//     everything in one place without setting env vars.
//  4. os.UserConfigDir() joined with "safedep/cli". Linux:
//     $XDG_CONFIG_HOME or ~/.config. macOS: ~/Library/Application Support.
//     Windows: %AppData% (Roaming).
//
// Returns an error when none of the above can be determined. The caller
// surfaces it so misconfigured environments fail loudly rather than
// writing to an unexpected location.
func dir() (string, error) {
	if v := os.Getenv(configDirEnvOverride); v != "" {
		return filepath.Join(v, configToolName), nil
	}

	if v := os.Getenv(xdgConfigHomeEnv); v != "" {
		return filepath.Join(v, configHomeRelative), nil
	}

	if home, err := os.UserHomeDir(); err == nil {
		xdgDefault := filepath.Join(home, ".config", configHomeRelative)
		if info, err := os.Stat(xdgDefault); err == nil && info.IsDir() {
			return xdgDefault, nil
		}
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: user config dir: %w", err)
	}
	return filepath.Join(base, configHomeRelative), nil
}
