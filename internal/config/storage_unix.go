//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// userDataDir resolves the platform-conventional base directory for
// durable application data on non-Windows OSes.
func userDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	return filepath.Join(home, ".local", "share"), nil
}
