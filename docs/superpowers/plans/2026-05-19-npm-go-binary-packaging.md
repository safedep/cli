# NPM + Go Binary Packaging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add npm packaging infrastructure to safedep-cli so `npx @safedep/cli setup mcp` works for first-time users.

**Architecture:** An ESM shim package (`@safedep/cli`) detects the host platform and spawns the Go binary from the matching optional platform package (`@safedep/cli-<os>-<arch>`). GoReleaser builds multi-platform binaries; a Go sync script reads `dist/artifacts.json` and copies them into the platform packages. Nx orchestrates the full chain.

**Tech Stack:** Go 1.26.2, goreleaser v2, pnpm workspaces, Nx, TypeScript, tsdown

**Spec:** `docs/superpowers/specs/2026-05-19-npm-go-binary-packaging-design.md`

---

## File Map

### New files
| File | Purpose |
| --- | --- |
| `go.work` | Go workspace linking `.` and `./scripts` |
| `package.json` | Private root npm workspace package |
| `pnpm-workspace.yaml` | Declares `packages/*` as workspace members |
| `nx.json` | Nx workspace config |
| `project.json` | Nx project for root (`safedep-cli` targets) |
| `packages/cli/package.json` | `@safedep/cli` meta package |
| `packages/cli/src/bin.ts` | ESM shim — detects platform, spawns binary |
| `packages/cli/tsdown.config.ts` | tsdown build config (CJS output) |
| `packages/cli/tsconfig.json` | TypeScript config for shim |
| `packages/cli/project.json` | Nx `build` + `typecheck` targets |
| `packages/cli-linux-x64/package.json` | Platform package |
| `packages/cli-linux-arm64/package.json` | Platform package |
| `packages/cli-darwin-x64/package.json` | Platform package |
| `packages/cli-darwin-arm64/package.json` | Platform package |
| `packages/cli-win32-x64/package.json` | Platform package |
| `packages/smoke/package.json` | Private smoke test consumer |
| `packages/smoke/project.json` | Nx `verify` target |
| `scripts/go.mod` | Shared Go module for build scripts |
| `scripts/sync-binaries/main.go` | Copies goreleaser output → platform packages |
| `scripts/check-version-sync/main.go` | Asserts all npm package versions match |
| `scripts/sync-binaries/project.json` | Nx `run` target |

### Modified files
| File | Change |
| --- | --- |
| `.goreleaser.yaml` | Add explicit `goarch`, `ignore` for windows/arm64 |
| `.gitignore` | Add `packages/*/bin/` |

---

## Task 1: Go workspace and .gitignore

**Files:**
- Create: `go.work`
- Modify: `.gitignore`

- [ ] **Step 1: Create `go.work`**

```text
go 1.26.2

use .
use ./scripts
```

- [ ] **Step 2: Add `packages/*/bin/` to `.gitignore`**

Open `.gitignore` and append after the existing `bin/` line:

```
packages/*/bin/
```

- [ ] **Step 3: Verify Go workspace**

```bash
go build ./...
```

Expected: exits 0 (same as before — `./scripts` doesn't exist yet but go.work tolerates missing `use` paths until referenced).

- [ ] **Step 4: Commit**

```bash
git add go.work .gitignore
git commit -m "chore: add go workspace and gitignore for npm packaging"
```

---

## Task 2: Root npm workspace scaffold

**Files:**
- Create: `package.json`
- Create: `pnpm-workspace.yaml`
- Create: `nx.json`

Prerequisite: `pnpm` must be installed (`npm install -g pnpm` or via corepack).

- [ ] **Step 1: Create root `package.json`**

```json
{
  "name": "safedep-cli-workspace",
  "private": true,
  "devDependencies": {
    "nx": "latest"
  }
}
```

- [ ] **Step 2: Create `pnpm-workspace.yaml`**

```yaml
packages:
  - 'packages/*'
```

- [ ] **Step 3: Create `nx.json`**

```json
{
  "$schema": "./node_modules/nx/schemas/nx-schema.json",
  "defaultBase": "main"
}
```

- [ ] **Step 4: Install dependencies**

```bash
pnpm install
```

Expected: creates `pnpm-lock.yaml` and `node_modules/`.

- [ ] **Step 5: Verify Nx is runnable**

```bash
pnpm nx --version
```

Expected: prints an Nx version number.

- [ ] **Step 6: Commit**

```bash
git add package.json pnpm-workspace.yaml nx.json pnpm-lock.yaml
git commit -m "chore: add pnpm workspace and Nx scaffold"
```

---

## Task 3: Platform npm packages

**Files:**
- Create: `packages/cli-linux-x64/package.json`
- Create: `packages/cli-linux-arm64/package.json`
- Create: `packages/cli-darwin-x64/package.json`
- Create: `packages/cli-darwin-arm64/package.json`
- Create: `packages/cli-win32-x64/package.json`

- [ ] **Step 1: Create `packages/cli-linux-x64/package.json`**

```json
{
  "name": "@safedep/cli-linux-x64",
  "version": "0.0.0",
  "os": ["linux"],
  "cpu": ["x64"],
  "files": ["bin/**"],
  "bin": {
    "safedep-cli": "bin/safedep"
  }
}
```

- [ ] **Step 2: Create `packages/cli-linux-arm64/package.json`**

```json
{
  "name": "@safedep/cli-linux-arm64",
  "version": "0.0.0",
  "os": ["linux"],
  "cpu": ["arm64"],
  "files": ["bin/**"],
  "bin": {
    "safedep-cli": "bin/safedep"
  }
}
```

- [ ] **Step 3: Create `packages/cli-darwin-x64/package.json`**

```json
{
  "name": "@safedep/cli-darwin-x64",
  "version": "0.0.0",
  "os": ["darwin"],
  "cpu": ["x64"],
  "files": ["bin/**"],
  "bin": {
    "safedep-cli": "bin/safedep"
  }
}
```

- [ ] **Step 4: Create `packages/cli-darwin-arm64/package.json`**

```json
{
  "name": "@safedep/cli-darwin-arm64",
  "version": "0.0.0",
  "os": ["darwin"],
  "cpu": ["arm64"],
  "files": ["bin/**"],
  "bin": {
    "safedep-cli": "bin/safedep"
  }
}
```

- [ ] **Step 5: Create `packages/cli-win32-x64/package.json`**

```json
{
  "name": "@safedep/cli-win32-x64",
  "version": "0.0.0",
  "os": ["win32"],
  "cpu": ["x64"],
  "files": ["bin/**"],
  "bin": {
    "safedep-cli": "bin/safedep.exe"
  }
}
```

- [ ] **Step 6: Re-run `pnpm install` to pick up new workspace members**

```bash
pnpm install
```

- [ ] **Step 7: Commit**

```bash
git add packages/cli-linux-x64 packages/cli-linux-arm64 packages/cli-darwin-x64 packages/cli-darwin-arm64 packages/cli-win32-x64 pnpm-lock.yaml
git commit -m "chore: add platform npm package stubs"
```

---

## Task 4: ESM shim package

**Files:**
- Create: `packages/cli/package.json`
- Create: `packages/cli/src/bin.ts`
- Create: `packages/cli/tsdown.config.ts`
- Create: `packages/cli/tsconfig.json`

- [ ] **Step 1: Create `packages/cli/package.json`**

```json
{
  "name": "@safedep/cli",
  "version": "0.0.0",
  "type": "module",
  "bin": {
    "safedep": "dist/bin.cjs"
  },
  "files": [
    "dist/**"
  ],
  "optionalDependencies": {
    "@safedep/cli-linux-x64":    "workspace:*",
    "@safedep/cli-linux-arm64":  "workspace:*",
    "@safedep/cli-darwin-x64":   "workspace:*",
    "@safedep/cli-darwin-arm64": "workspace:*",
    "@safedep/cli-win32-x64":    "workspace:*"
  },
  "scripts": {
    "build": "tsdown",
    "typecheck": "tsc -p tsconfig.json --noEmit"
  },
  "devDependencies": {
    "@types/node": "latest",
    "tsdown": "latest",
    "typescript": "latest"
  }
}
```

Note: `dist/bin.cjs` is CJS format. Despite the package being `"type": "module"`, the `.cjs` extension forces Node to treat the bin as CommonJS, which is the most compatible format for an npm bin entrypoint.

- [ ] **Step 2: Create `packages/cli/src/bin.ts`**

```typescript
#!/usr/bin/env node
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { existsSync } from "node:fs";
import { spawn } from "node:child_process";

const require = createRequire(import.meta.url);

function pkgNameForHost(): string {
  const platform = process.platform; // linux | darwin | win32
  const arch = process.arch;         // x64 | arm64
  return `@safedep/cli-${platform}-${arch}`;
}

function findBinaryPath(pkgName: string): string {
  const pkgJsonPath = require.resolve(`${pkgName}/package.json`);
  const pkgRoot = dirname(pkgJsonPath);
  const exe = process.platform === "win32" ? "safedep.exe" : "safedep";
  const p = join(pkgRoot, "bin", exe);
  if (!existsSync(p)) {
    throw new Error(
      `Binary not found at ${p}. The platform package "${pkgName}" is installed but does not contain bin/${exe}.`
    );
  }
  return p;
}

function main() {
  const pkgName = pkgNameForHost();
  let binPath: string;
  try {
    binPath = findBinaryPath(pkgName);
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    console.error(
      [
        "Failed to locate the platform binary.",
        `Host: ${process.platform}/${process.arch}`,
        `Expected platform package: ${pkgName}`,
        msg,
        "",
        "Common causes:",
        "- optionalDependencies were omitted during install",
        "- this platform/arch is not published yet",
        "- the platform package was published without the binary in bin/",
      ].join("\n")
    );
    process.exit(1);
  }

  const child = spawn(binPath, process.argv.slice(2), { stdio: "inherit" });

  child.on("exit", (code, signal) => {
    if (signal) process.kill(process.pid, signal);
    process.exit(code ?? 1);
  });
  child.on("error", (error) => {
    console.error(`Failed to spawn the binary: ${error}`);
    process.exit(1);
  });
}

main();
```

- [ ] **Step 3: Create `packages/cli/tsdown.config.ts`**

```typescript
import { defineConfig } from "tsdown";

export default defineConfig({
  entry: ["src/bin.ts"],
  format: ["cjs"],
  clean: true,
});
```

- [ ] **Step 4: Create `packages/cli/tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "skipLibCheck": true
  },
  "include": ["src/**/*"]
}
```

- [ ] **Step 5: Install shim package dependencies**

```bash
pnpm install
```

- [ ] **Step 6: Build the shim (no platform binaries needed for this step)**

```bash
pnpm --filter @safedep/cli run build
```

Expected: `packages/cli/dist/bin.cjs` is created.

- [ ] **Step 7: Typecheck**

```bash
pnpm --filter @safedep/cli run typecheck
```

Expected: exits 0, no errors.

- [ ] **Step 8: Commit**

```bash
git add packages/cli pnpm-lock.yaml
git commit -m "chore: add @safedep/cli ESM shim package"
```

---

## Task 5: Go scripts module

**Files:**
- Create: `scripts/go.mod`
- Create: `scripts/sync-binaries/main.go`
- Create: `scripts/check-version-sync/main.go`

**Key design note for sync-binaries:** The existing `.goreleaser.yaml` uses `universal_binaries: replace: true`, which merges `darwin/amd64` and `darwin/arm64` into a single macOS fat binary. This artifact appears in `dist/artifacts.json` with `"type": "Universal Binary"` (not `"Binary"`). The sync script must handle this type and copy the universal binary into BOTH `packages/cli-darwin-x64/bin/` and `packages/cli-darwin-arm64/bin/`.

- [ ] **Step 1: Create `scripts/go.mod`**

```
module github.com/safedep/cli/scripts

go 1.26.2
```

- [ ] **Step 2: Create `scripts/sync-binaries/main.go`**

```go
// Sync binaries to packages directory from goreleaser's dist/ directory.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/go-playground/validator/v10"
)

type GoreleaserArtifact struct {
	Path   string `json:"path"   validate:"required"`
	Goos   string `json:"goos"   validate:"required"`
	Goarch string `json:"goarch"`
	Type   string `json:"type"   validate:"required"`
}

var goArchToNodeArchMap = map[string]string{
	"amd64": "x64",
	"386":   "x86",
	"arm64": "arm64",
}

var goOsToNodeOsMap = map[string]string{
	"windows": "win32",
}

func main() {
	artifactsPath := flag.String("artifacts-path", "dist/artifacts.json", "Path to goreleaser artifacts.json")
	packagesPath := flag.String("packages-path", "./packages", "Path to the npm packages directory")
	strict := flag.Bool("strict", true, "Fail if a package directory does not exist for a built artifact")
	flag.Parse()

	artifactsBytes, err := os.ReadFile(*artifactsPath)
	if err != nil {
		log.Fatalf("failed to read artifacts.json (did you run goreleaser build?): %v", err)
	}

	var artifacts []GoreleaserArtifact
	if err := json.Unmarshal(artifactsBytes, &artifacts); err != nil {
		log.Fatalf("failed to parse artifacts.json: %v", err)
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	for _, artifact := range artifacts {
		switch artifact.Type {
		case "Binary":
			if err := validate.Struct(artifact); err != nil {
				log.Printf("skipping invalid artifact: %v", err)
				continue
			}
			if err := syncBinary(artifact, *packagesPath, *strict); err != nil {
				log.Fatalf("sync: %v", err)
			}

		case "Universal Binary":
			// goreleaser merges darwin/amd64 + darwin/arm64 into a single fat
			// binary when universal_binaries.replace is true. Copy it to both
			// darwin platform packages since it runs on either architecture.
			if artifact.Goos != "darwin" {
				log.Printf("unexpected universal binary for goos=%s, skipping", artifact.Goos)
				continue
			}
			for _, nodeArch := range []string{"x64", "arm64"} {
				packagePath := filepath.Join(*packagesPath, fmt.Sprintf("cli-darwin-%s", nodeArch))
				if err := copyToBin(artifact.Path, packagePath, "safedep", *strict); err != nil {
					log.Fatalf("sync darwin universal → %s: %v", nodeArch, err)
				}
			}
		}
	}
}

func syncBinary(artifact GoreleaserArtifact, packagesPath string, strict bool) error {
	nodeArch, ok := goArchToNodeArchMap[artifact.Goarch]
	if !ok {
		nodeArch = artifact.Goarch
	}
	nodeOs, ok := goOsToNodeOsMap[artifact.Goos]
	if !ok {
		nodeOs = artifact.Goos
	}

	packagePath := filepath.Join(packagesPath, fmt.Sprintf("cli-%s-%s", nodeOs, nodeArch))
	binName := "safedep"
	if artifact.Goos == "windows" {
		binName = "safedep.exe"
	}
	return copyToBin(artifact.Path, packagePath, binName, strict)
}

func copyToBin(src, packagePath, binName string, strict bool) error {
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		if strict {
			return fmt.Errorf("package directory %s does not exist (add the platform package or remove the goreleaser target)", packagePath)
		}
		log.Printf("package directory %s does not exist, skipping", packagePath)
		return nil
	}

	binDir := filepath.Join(packagePath, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir %s: %w", binDir, err)
	}

	dst := filepath.Join(binDir, binName)
	log.Printf("copying %s → %s", src, dst)
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	return os.Chmod(dst, srcInfo.Mode())
}
```

- [ ] **Step 3: Create `scripts/check-version-sync/main.go`**

```go
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
```

- [ ] **Step 4: Add the validator dependency**

```bash
cd scripts && go get github.com/go-playground/validator/v10 && go mod tidy && cd ..
```

- [ ] **Step 5: Verify scripts compile**

```bash
go build ./scripts/...
```

Expected: exits 0, produces no output (binaries go to module cache, not disk).

- [ ] **Step 6: Verify go.work covers scripts**

```bash
go run ./scripts/check-version-sync/ --packages-path ./packages
```

Expected: prints version info for all 5 platform packages (all at `0.0.0`) and exits 0.

- [ ] **Step 7: Commit**

```bash
git add scripts/ go.work
git commit -m "chore: add Go scripts module for binary sync and version check"
```

---

## Task 6: GoReleaser platform matrix update

**Files:**
- Modify: `.goreleaser.yaml`

The current config builds `linux`, `windows`, and `darwin` but has no explicit `goarch`, relying on goreleaser's default of `[386, amd64, arm64]`. We add `goarch: [amd64, arm64]` to remove 386 builds (no 386 npm package) and explicitly exclude `windows/arm64`.

`universal_binaries: replace: true` is already present — it merges `darwin/amd64` and `darwin/arm64` into one fat binary. No change needed there.

- [ ] **Step 1: Update the `builds` section in `.goreleaser.yaml`**

Change:

```yaml
builds:
  - main: ./cmd/safedep
    binary: safedep
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - windows
      - darwin
    ldflags:
```

To:

```yaml
builds:
  - main: ./cmd/safedep
    binary: safedep
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - windows
      - darwin
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
```

- [ ] **Step 2: Verify config is valid**

```bash
goreleaser check
```

Expected: `config is valid` (goreleaser must be installed: `go install github.com/goreleaser/goreleaser/v2@latest`).

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yaml
git commit -m "chore: add arm64 and darwin to goreleaser platform matrix"
```

---

## Task 7: Nx project configs

**Files:**
- Create: `project.json` (root)
- Create: `scripts/sync-binaries/project.json`
- Create: `packages/cli/project.json`

Note: `packages/smoke/project.json` is created in Task 8 alongside its `package.json` — Nx must not discover a project.json before the corresponding package.json exists.

- [ ] **Step 1: Create root `project.json`**

This defines the `safedep-cli` Nx project with the goreleaser snapshot build and the top-level orchestrator targets.

```json
{
  "name": "safedep-cli",
  "root": ".",
  "targets": {
    "build-snapshot": {
      "executor": "nx:run-commands",
      "options": {
        "command": "goreleaser build --clean --snapshot",
        "cwd": "{workspaceRoot}"
      },
      "inputs": [
        "{workspaceRoot}/**/*.go",
        "{workspaceRoot}/go.mod",
        "{workspaceRoot}/go.sum",
        "{workspaceRoot}/.goreleaser.yaml",
        "!{workspaceRoot}/packages/**",
        "!{workspaceRoot}/scripts/**",
        "!{workspaceRoot}/dist/**",
        "!{workspaceRoot}/bin/**",
        "!{workspaceRoot}/node_modules/**"
      ],
      "outputs": ["{workspaceRoot}/dist"]
    },
    "build-dev": {
      "executor": "nx:run-commands",
      "dependsOn": [{"projects": "@safedep/cli", "target": "build"}],
      "options": {
        "command": "echo 'build-dev complete'"
      }
    },
    "verify": {
      "executor": "nx:run-commands",
      "dependsOn": [{"projects": "smoke", "target": "verify"}],
      "options": {
        "command": "echo 'verify complete'"
      }
    }
  }
}
```

- [ ] **Step 2: Create `scripts/sync-binaries/project.json`**

```json
{
  "name": "sync-binaries",
  "root": "scripts/sync-binaries",
  "targets": {
    "run": {
      "executor": "nx:run-commands",
      "dependsOn": [{"projects": "safedep-cli", "target": "build-snapshot"}],
      "options": {
        "command": "go run ./scripts/sync-binaries/ --strict --artifacts-path dist/artifacts.json --packages-path ./packages",
        "cwd": "{workspaceRoot}"
      },
      "inputs": [
        "{workspaceRoot}/scripts/sync-binaries/**/*.go",
        "{workspaceRoot}/scripts/go.mod",
        "{workspaceRoot}/scripts/go.sum",
        "{workspaceRoot}/dist/artifacts.json"
      ],
      "outputs": [
        "{workspaceRoot}/packages/cli-linux-x64/bin",
        "{workspaceRoot}/packages/cli-linux-arm64/bin",
        "{workspaceRoot}/packages/cli-darwin-x64/bin",
        "{workspaceRoot}/packages/cli-darwin-arm64/bin",
        "{workspaceRoot}/packages/cli-win32-x64/bin"
      ]
    }
  }
}
```

- [ ] **Step 4: Create `packages/cli/project.json`**

```json
{
  "name": "@safedep/cli",
  "root": "packages/cli",
  "targets": {
    "build": {
      "executor": "nx:run-commands",
      "dependsOn": [{"projects": "sync-binaries", "target": "run"}],
      "options": {
        "command": "pnpm run build",
        "cwd": "{projectRoot}"
      },
      "inputs": [
        "{projectRoot}/src/**",
        "{projectRoot}/tsdown.config.ts",
        "{projectRoot}/tsconfig.json"
      ],
      "outputs": ["{projectRoot}/dist"]
    },
    "typecheck": {
      "executor": "nx:run-commands",
      "options": {
        "command": "pnpm run typecheck",
        "cwd": "{projectRoot}"
      },
      "inputs": [
        "{projectRoot}/src/**",
        "{projectRoot}/tsconfig.json"
      ]
    }
  }
}
```

- [ ] **Step 5: Commit**

```bash
git add project.json scripts/sync-binaries/project.json packages/cli/project.json
git commit -m "chore: add Nx project configs and orchestration targets"
```

---

## Task 8: Smoke package

**Files:**
- Create: `packages/smoke/package.json`
- Create: `packages/smoke/project.json`

Both files are created together so Nx never discovers a project.json without its package.json.

- [ ] **Step 1: Create `packages/smoke/package.json`**

```json
{
  "name": "smoke",
  "private": true,
  "dependencies": {
    "@safedep/cli": "workspace:*"
  }
}
```

- [ ] **Step 2: Create `packages/smoke/project.json`**

```json
{
  "name": "smoke",
  "root": "packages/smoke",
  "targets": {
    "verify": {
      "executor": "nx:run-commands",
      "dependsOn": [{"projects": "safedep-cli", "target": "build-dev"}],
      "options": {
        "command": "pnpm exec safedep --version",
        "cwd": "{workspaceRoot}/packages/smoke"
      }
    }
  }
}
```

- [ ] **Step 3: Run `pnpm install` to link the smoke package**

```bash
pnpm install
```

- [ ] **Step 4: Commit**

```bash
git add packages/smoke pnpm-lock.yaml
git commit -m "chore: add smoke test package"
```

---

## Task 9: End-to-end pipeline verification

Prerequisites:
- `goreleaser` installed (`go install github.com/goreleaser/goreleaser/v2@latest`)
- All previous tasks complete

- [ ] **Step 1: Run the full build pipeline**

```bash
pnpm nx run safedep-cli:build-dev
```

Expected output sequence:
1. `goreleaser build --clean --snapshot` — builds binaries for all 5 targets
2. `go run ./scripts/sync-binaries/ --strict ...` — copies binaries to `packages/*/bin/`
3. `pnpm run build` in `packages/cli` — produces `packages/cli/dist/bin.cjs`

- [ ] **Step 2: Verify binaries were copied**

```bash
ls packages/cli-linux-x64/bin/
ls packages/cli-linux-arm64/bin/
ls packages/cli-darwin-x64/bin/
ls packages/cli-darwin-arm64/bin/
ls packages/cli-win32-x64/bin/
```

Expected: each directory contains `safedep` (or `safedep.exe` for win32).

- [ ] **Step 3: Verify the shim executes the binary on the current platform**

```bash
pnpm --filter smoke exec safedep --version
```

Expected: prints the safedep version string (from `dist/safedep_..._snapshot/safedep --version` via the shim).

- [ ] **Step 4: Run the smoke verify Nx target**

```bash
pnpm nx run safedep-cli:verify
```

Expected: `smoke:verify` passes, prints version string, exits 0.

- [ ] **Step 5: Verify Nx caching**

Run `build-dev` a second time without changing anything:

```bash
pnpm nx run safedep-cli:build-dev
```

Expected: all targets show `[local cache]` — no commands re-run.

- [ ] **Step 6: Verify version sync**

```bash
go run ./scripts/check-version-sync/ --packages-path ./packages
```

Expected: all 5 non-private packages at `0.0.0`, exits 0.

- [ ] **Step 7: Final commit**

```bash
git add .
git commit -m "chore: verify npm packaging pipeline end-to-end"
```

---

## Notes

**Nx cache invalidation on goreleaser:** Nx caches `safedep-cli:build-snapshot` based on Go source file inputs. Any change to `*.go`, `go.mod`, `go.sum`, or `.goreleaser.yaml` invalidates the cache and re-runs goreleaser.

**darwin universal binary:** The `packages/cli-darwin-x64/bin/safedep` and `packages/cli-darwin-arm64/bin/safedep` are the same fat binary (Mach-O universal). Both run on Intel and Apple Silicon. The platform packages present them as separate packages to satisfy npm's `cpu` constraint filtering.

**Deferred:** Release automation, npm publish, macOS notarization, `check-version-sync --require-tag`.
