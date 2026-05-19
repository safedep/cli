package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePkgJSON(t *testing.T, dir, name string, content map[string]any) string {
	t.Helper()
	pkgDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	data, err := json.MarshalIndent(content, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(pkgDir, "package.json")
	require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o644))
	return path
}

func readVersion(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var pkg map[string]any
	require.NoError(t, json.Unmarshal(data, &pkg))
	v, _ := pkg["version"].(string)
	return v
}

func TestSetPackageVersions(t *testing.T) {
	t.Run("sets version in all non-private packages", func(t *testing.T) {
		dir := t.TempDir()
		pathA := writePkgJSON(t, dir, "pkg-a", map[string]any{"name": "pkg-a", "version": "0.0.0"})
		pathB := writePkgJSON(t, dir, "pkg-b", map[string]any{"name": "pkg-b", "version": "0.0.0"})

		require.NoError(t, setPackageVersions(dir, "1.2.3"))

		assert.Equal(t, "1.2.3", readVersion(t, pathA))
		assert.Equal(t, "1.2.3", readVersion(t, pathB))
	})

	t.Run("skips private packages", func(t *testing.T) {
		dir := t.TempDir()
		pathPriv := writePkgJSON(t, dir, "private-pkg", map[string]any{
			"name":    "private-pkg",
			"version": "0.0.0",
			"private": true,
		})
		pathPub := writePkgJSON(t, dir, "public-pkg", map[string]any{"name": "public-pkg", "version": "0.0.0"})

		require.NoError(t, setPackageVersions(dir, "2.0.0"))

		assert.Equal(t, "0.0.0", readVersion(t, pathPriv))
		assert.Equal(t, "2.0.0", readVersion(t, pathPub))
	})

	t.Run("skips subdirectories without package.json", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "no-pkg-json"), 0o755))
		pathA := writePkgJSON(t, dir, "pkg-a", map[string]any{"name": "pkg-a", "version": "0.0.0"})

		require.NoError(t, setPackageVersions(dir, "3.0.0"))

		assert.Equal(t, "3.0.0", readVersion(t, pathA))
	})

	t.Run("rejects invalid semver", func(t *testing.T) {
		err := setPackageVersions(t.TempDir(), "not-a-version")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid version")
	})

	t.Run("returns error when packages dir is missing", func(t *testing.T) {
		err := setPackageVersions("/nonexistent/path", "1.0.0")
		require.Error(t, err)
	})
}

func TestSetVersionInPackageJSON(t *testing.T) {
	t.Run("writes version field", func(t *testing.T) {
		dir := t.TempDir()
		path := writePkgJSON(t, dir, "pkg", map[string]any{"name": "pkg", "version": "0.0.0"})

		require.NoError(t, setVersionInPackageJSON(filepath.Join(dir, "pkg", "package.json"), "4.5.6"))

		assert.Equal(t, "4.5.6", readVersion(t, path))
	})

	t.Run("skips private package", func(t *testing.T) {
		dir := t.TempDir()
		path := writePkgJSON(t, dir, "pkg", map[string]any{"name": "pkg", "version": "0.0.0", "private": true})

		require.NoError(t, setVersionInPackageJSON(filepath.Join(dir, "pkg", "package.json"), "4.5.6"))

		assert.Equal(t, "0.0.0", readVersion(t, path))
	})

	t.Run("missing file is a no-op", func(t *testing.T) {
		err := setVersionInPackageJSON("/nonexistent/package.json", "1.0.0")
		require.NoError(t, err)
	})

	t.Run("preserves inline array formatting", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "pkg", "package.json")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

		// Write a package.json with compact inline arrays, matching the real
		// platform packages under packages/cli-*/package.json.
		original := `{
  "name": "pkg",
  "version": "0.0.0",
  "os": ["linux"],
  "cpu": ["x64"],
  "files": ["bin/**"]
}
`
		require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

		require.NoError(t, setVersionInPackageJSON(path, "1.2.3"))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		content := string(data)

		assert.Contains(t, content, `"version": "1.2.3"`)
		assert.Contains(t, content, `"os": ["linux"]`)
		assert.Contains(t, content, `"cpu": ["x64"]`)
		assert.Contains(t, content, `"files": ["bin/**"]`)
	})

	t.Run("does not match version inside a string value", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "pkg", "package.json")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

		// A description that contains the text "version": "..." mid-line must
		// not be rewritten. The multiline anchor prevents this: "version"
		// appears after "description": "..., not at the start of a line.
		original := "{\n  \"name\": \"pkg\",\n  \"version\": \"0.0.0\",\n  \"description\": \"see \\\"version\\\": \\\"1.0.0\\\" in docs\"\n}\n"
		require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

		require.NoError(t, setVersionInPackageJSON(path, "2.0.0"))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		content := string(data)

		assert.Contains(t, content, `"version": "2.0.0"`)
		// The description value is stored as JSON with backslash-escaped quotes;
		// assert the raw bytes are unchanged (no rewrite occurred inside the string).
		assert.Contains(t, content, "\"description\": \"see \\\"version\\\": \\\"1.0.0\\\" in docs\"")
	})
}
