package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// versionFieldRe is used to swap the "version" field in raw JSON bytes instead
// of round-tripping through a Go struct, which would re-serialize arrays and
// destroy inline formatting (e.g. "os": ["linux"] would expand to multi-line).
// Anchoring to start-of-line prevents false matches inside string values of
// other keys. Group 1 captures leading whitespace so indentation is unchanged.
var versionFieldRe = regexp.MustCompile(`(?m)^(\s*)"version"\s*:\s*"[^"]*"`)

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

	repl := fmt.Appendf(nil, "${1}\"version\": \"%s\"", version)
	updated := versionFieldRe.ReplaceAll(data, repl)

	return os.WriteFile(path, updated, 0o644)
}

// verifyPackageBins checks that every platform package under packagesPath has a
// non-empty bin/ directory. Platform packages are identified by the presence of
// an "os" field in their package.json; packages without that field (e.g. the
// meta/shim package) and private packages are skipped.
func verifyPackageBins(packagesPath string) error {
	entries, err := os.ReadDir(packagesPath)
	if err != nil {
		return fmt.Errorf("read packages dir: %w", err)
	}

	var missing []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pkgJSONPath := filepath.Join(packagesPath, entry.Name(), "package.json")
		data, err := os.ReadFile(pkgJSONPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", pkgJSONPath, err)
		}

		var pkg map[string]json.RawMessage
		if err := json.Unmarshal(data, &pkg); err != nil {
			return fmt.Errorf("parse %s: %w", pkgJSONPath, err)
		}

		// Skip private packages.
		if priv, ok := pkg["private"]; ok {
			var b bool
			if json.Unmarshal(priv, &b) == nil && b {
				continue
			}
		}

		// Only platform packages declare an "os" constraint.
		if _, ok := pkg["os"]; !ok {
			continue
		}

		binDir := filepath.Join(packagesPath, entry.Name(), "bin")
		binEntries, err := os.ReadDir(binDir)
		if err != nil || len(binEntries) == 0 {
			missing = append(missing, entry.Name())
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("platform packages missing bin/: %s", strings.Join(missing, ", "))
	}
	return nil
}
