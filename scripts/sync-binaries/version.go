package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// setPackageVersions scans every immediate subdirectory of packagesPath for a
// package.json, skips those with "private": true, and writes version to the
// rest. Returns an error on the first failure.
func setPackageVersions(packagesPath, version string) error {
	if !semverRe.MatchString(version) {
		return fmt.Errorf("invalid version %q: must be x.y.z", version)
	}

	entries, err := os.ReadDir(packagesPath)
	if err != nil {
		return fmt.Errorf("read packages dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(packagesPath, entry.Name(), "package.json")
		if err := setVersionInPackageJSON(path, version); err != nil {
			return fmt.Errorf("package %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// setVersionInPackageJSON reads the file at path, sets "version" to version,
// and writes it back. A missing file is silently skipped. Packages with
// "private": true are skipped unchanged.
func setVersionInPackageJSON(path, version string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	if priv, ok := pkg["private"].(bool); ok && priv {
		return nil
	}

	pkg["version"] = version

	out, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	return os.WriteFile(path, append(out, '\n'), 0o644)
}
