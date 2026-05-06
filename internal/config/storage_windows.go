//go:build windows

package config

import (
	"fmt"
	"os"
)

// userDataDir resolves %LOCALAPPDATA%, the conventional Windows
// location for durable per-user application data.
func userDataDir() (string, error) {
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("LOCALAPPDATA not set")
}
