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

// versionFieldRe matches a JSON "version" key-value pair. The captured group
// is the full match so ReplaceAll can swap just the value.
var versionFieldRe = regexp.MustCompile(`"version"\s*:\s*"[^"]*"`)

// setVersionInPackageJSON reads the file at path, sets "version" to version,
// and writes it back. A missing file is silently skipped. Packages with
// "private": true are skipped unchanged.
//
// The replacement is done on the raw bytes so all other formatting (key order,
// inline arrays, whitespace) is preserved byte-for-byte.
func setVersionInPackageJSON(path, version string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var pkg map[string]json.RawMessage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	if priv, ok := pkg["private"]; ok {
		var b bool
		if json.Unmarshal(priv, &b) == nil && b {
			return nil
		}
	}

	replacement := fmt.Sprintf(`"version": %q`, version)
	updated := versionFieldRe.ReplaceAll(data, []byte(replacement))

	return os.WriteFile(path, updated, 0o644)
}
