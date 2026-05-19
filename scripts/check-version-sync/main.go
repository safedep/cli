// check-version-sync verifies that all non-private npm packages in the
// packages/ directory are at the same version, and optionally that the version
// matches the current git tag on HEAD.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type packageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Private bool   `json:"private"`
}

func readPackageJSON(path string) (*packageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &pkg, nil
}

func main() {
	packagesPath := flag.String("packages-path", "./packages", "Path to the packages directory")
	requireTag := flag.Bool("require-tag", false, "Fail if no exact git tag on HEAD matches the npm version")
	flag.Parse()

	entries, err := os.ReadDir(*packagesPath)
	if err != nil {
		log.Fatalf("failed to read packages directory %s: %v", *packagesPath, err)
	}

	type entry struct {
		name    string
		version string
	}
	var packages []entry

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkgPath := filepath.Join(*packagesPath, e.Name(), "package.json")
		pkg, err := readPackageJSON(pkgPath)
		if err != nil {
			log.Fatalf("error: %v", err)
		}
		if pkg.Private {
			log.Printf("skipping private package: %s", e.Name())
			continue
		}
		if pkg.Version == "" {
			log.Fatalf("package %s has no version field in %s", e.Name(), pkgPath)
		}
		log.Printf("%s: %s", pkg.Name, pkg.Version)
		packages = append(packages, entry{name: pkg.Name, version: pkg.Version})
	}

	if len(packages) == 0 {
		log.Fatalf("no non-private packages found in %s", *packagesPath)
	}

	canonical := packages[0].version
	for _, p := range packages[1:] {
		if p.version != canonical {
			log.Fatalf("version mismatch: %s is at %s but %s is at %s",
				packages[0].name, canonical, p.name, p.version)
		}
	}
	log.Printf("all %d packages are at version: %s", len(packages), canonical)

	if !*requireTag {
		log.Printf("version sync check passed")
		return
	}

	out, err := exec.Command("git", "describe", "--tags", "--exact-match", "HEAD").Output()
	if err != nil {
		log.Fatalf(
			"no exact git tag on HEAD: expected tag v%s\ncreate it with: git tag v%s && git push origin v%s",
			canonical, canonical, canonical,
		)
	}
	gitTag := strings.TrimSpace(string(out))
	tagVersion := strings.TrimPrefix(gitTag, "v")
	if tagVersion != canonical {
		log.Fatalf("git tag %s does not match npm version %s", gitTag, canonical)
	}
	log.Printf("git tag %s matches npm version %s", gitTag, canonical)
	log.Printf("version sync check passed")
}
