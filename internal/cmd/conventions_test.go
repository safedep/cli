package cmd_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cmdPkgPrefix = "github.com/safedep/cli/internal/cmd/"

// rootLevelExceptions are leaves intentionally permitted at depth 1,
// against the noun-verb shape rule. Universal CLI conventions trump our
// rule for a small set of well-known commands. See DEVGUIDE Command shape.
var rootLevelExceptions = map[string]struct{}{
	"version": {},
}

// repoRoot resolves the repository root by walking up from this file's
// location. The lints inspect docs/ and README.md, so we need a real path
// independent of the test's CWD.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// internal/cmd/conventions_test.go: repo root is two levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// walk yields every command in the tree paired with its path tokens
// (excluding the root "safedep" itself).
func walk(root *cobra.Command, fn func(c *cobra.Command, path []string)) {
	var visit func(c *cobra.Command, path []string)
	visit = func(c *cobra.Command, path []string) {
		fn(c, path)
		for _, sub := range c.Commands() {
			visit(sub, append(path, sub.Name()))
		}
	}
	for _, sub := range root.Commands() {
		visit(sub, []string{sub.Name()})
	}
}

// isCobraGenerated reports whether c is a cobra-injected helper (help,
// completion). These bypass our conventions and are exempt.
func isCobraGenerated(c *cobra.Command) bool {
	switch c.Name() {
	case "help", "completion":
		return true
	}
	if c.Parent() != nil && c.Parent().Name() == "completion" {
		return true
	}
	return false
}

// isLeaf reports whether c executes a command (vs. acting as a parent
// noun). Cobra parents have no Run/RunE. Leaves have one.
func isLeaf(c *cobra.Command) bool {
	return c.RunE != nil || c.Run != nil
}

func newTree(t *testing.T) *cobra.Command {
	t.Helper()
	a := app.New(&config.Config{})
	t.Cleanup(a.Close)
	return cmd.NewSafedep(a)
}

func TestConventions_LeafShape(t *testing.T) {
	root := newTree(t)
	walk(root, func(c *cobra.Command, path []string) {
		if isCobraGenerated(c) || !isLeaf(c) {
			return
		}
		full := strings.Join(append([]string{"safedep"}, path...), " ")
		t.Run(full, func(t *testing.T) {
			if len(path) == 1 {
				if _, ok := rootLevelExceptions[path[0]]; ok {
					return
				}
			}

			require.GreaterOrEqual(t, len(path), 2,
				"leaf %q must be at depth >= 2 (noun/verb); got %v", full, path)

			verb := path[len(path)-1]
			assert.True(t, cmd.IsAllowedVerb(verb),
				"verb %q not in allow-list (internal/cmd/verbs.go); leaf=%q", verb, full)
		})
	})
}

func TestConventions_NoHyphensInUse(t *testing.T) {
	root := newTree(t)
	walk(root, func(c *cobra.Command, path []string) {
		if isCobraGenerated(c) {
			return
		}
		// c.Name() is the first token of Use. That is what enters the path.
		assert.NotContains(t, c.Name(), "-",
			"command name must not contain hyphens: path=%v", path)
	})
}

func TestConventions_ShortAndLongPresent(t *testing.T) {
	root := newTree(t)
	walk(root, func(c *cobra.Command, path []string) {
		if isCobraGenerated(c) {
			return
		}
		full := strings.Join(append([]string{"safedep"}, path...), " ")
		t.Run(full, func(t *testing.T) {
			assert.NotEmpty(t, c.Short, "%q: Short must be non-empty", full)
			assert.NotEmpty(t, c.Long, "%q: Long must be non-empty", full)
		})
	})
}

func TestConventions_LeafDocPagesExist(t *testing.T) {
	root := newTree(t)
	docsDir := filepath.Join(repoRoot(t), "docs", "cmd")

	walk(root, func(c *cobra.Command, path []string) {
		if isCobraGenerated(c) || !isLeaf(c) {
			return
		}
		full := strings.Join(append([]string{"safedep"}, path...), " ")
		docName := strings.Join(path, "-") + ".md"
		t.Run(full, func(t *testing.T) {
			docPath := filepath.Join(docsDir, docName)
			_, err := os.Stat(docPath)
			assert.NoError(t, err,
				"expected doc page at docs/cmd/%s for %q", docName, full)
		})
	})
}

// TestConventions_NoCrossCmdImports asserts that no Go file under
// internal/cmd/<x>/... imports internal/cmd/<y>/... for x != y. The
// boundary is the immediate child of internal/cmd. internal/cmd itself
// (the parent package, where NewSafedep lives) is allowed to import any
// child.
func TestConventions_NoCrossCmdImports(t *testing.T) {
	cmdRoot := filepath.Join(repoRoot(t), "internal", "cmd")

	err := filepath.WalkDir(cmdRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, err := filepath.Rel(cmdRoot, path)
		require.NoError(t, err)

		segs := strings.Split(filepath.ToSlash(rel), "/")
		if len(segs) < 2 {
			// File directly under internal/cmd (verbs.go, root.go, etc.) is the
			// parent package. Cross-cmd imports there are intentional.
			return nil
		}
		owner := segs[0]
		ownerPrefix := cmdPkgPrefix + owner

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		require.NoErrorf(t, err, "parse %s", path)

		for _, imp := range f.Imports {
			pkg := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(pkg, cmdPkgPrefix) {
				continue
			}
			if pkg == ownerPrefix || strings.HasPrefix(pkg, ownerPrefix+"/") {
				continue
			}
			t.Errorf("%s imports sibling cmd package %q (owner=%s)", rel, pkg, owner)
		}
		return nil
	})
	require.NoError(t, err)
}

func TestConventions_ReadmeLinksAllDocPages(t *testing.T) {
	root := newTree(t)
	readmePath := filepath.Join(repoRoot(t), "README.md")
	readmeBytes, err := os.ReadFile(readmePath)
	require.NoError(t, err, "README.md must exist")
	readme := string(readmeBytes)

	walk(root, func(c *cobra.Command, path []string) {
		if isCobraGenerated(c) || !isLeaf(c) {
			return
		}
		full := strings.Join(append([]string{"safedep"}, path...), " ")
		link := "docs/cmd/" + strings.Join(path, "-") + ".md"
		t.Run(full, func(t *testing.T) {
			assert.Contains(t, readme, link,
				"README.md must link to %s for %q", link, full)
		})
	})
}
