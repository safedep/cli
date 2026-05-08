# Integration JFrog Run Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `safedep integration jfrog run --config <file>` — a daemon that polls SafeDep's malicious package API and pushes verified findings to JFrog XRay as Custom Issues.

**Architecture:** A `FeedService` ties together a `MaliciousPackagePoller` (gRPC polling with page-token pagination) and a `JFrogPusher` (HTTP POST to XRay). A file-based cursor persists the `last_seen_at` timestamp across restarts. Auth flows entirely through the existing `a.DataPlane()` accessor.

**Tech Stack:** Go, cobra, `buf.build/gen/go/safedep/api` (grpc + protocolbuffers), `gopkg.in/yaml.v3`, `net/http`, `encoding/json`

**Branch:** `feat/integration-jfrog-run`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/cmd/integration/cmd.go` | Create | Registers `integration` parent cobra command |
| `internal/cmd/integration/jfrog/cmd.go` | Create | Registers `jfrog` sub-command under `integration` |
| `internal/cmd/integration/jfrog/config.go` | Create | YAML config schema, loader, validation |
| `internal/cmd/integration/jfrog/store.go` | Create | File-based cursor (read/write JSON, atomic rename) |
| `internal/cmd/integration/jfrog/pusher.go` | Create | HTTP POST to JFrog XRay `/xray/api/v1/events` |
| `internal/cmd/integration/jfrog/poller.go` | Create | gRPC poll loop with pagination and cursor save |
| `internal/cmd/integration/jfrog/service.go` | Create | `FeedService`: poll → push orchestration loop |
| `internal/cmd/integration/jfrog/run.go` | Create | Cobra `run` command, `RunE` wiring only |
| `internal/cmd/safedep.go` | Modify | Add `integration.Register(root, a)` |
| `docs/cmd/integration-jfrog-run.md` | Create | Leaf command doc page (required by DEVGUIDE) |
| `README.md` | Modify | Add link to doc page (required by DEVGUIDE) |

---

## Task 1: Config

**Files:**
- Create: `internal/cmd/integration/jfrog/config.go`

- [ ] **Create the config file**

```go
// internal/cmd/integration/jfrog/config.go
package jfrog

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Source SourceConfig `yaml:"source"`
	JFrog  JFrogConfig  `yaml:"jfrog"`
}

type SourceConfig struct {
	PollInterval time.Duration `yaml:"poll_interval"`
	CursorFile   string        `yaml:"cursor_file"`
}

type JFrogConfig struct {
	URL         string `yaml:"url"`
	AccessToken string `yaml:"access_token"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}

	if cfg.JFrog.URL == "" {
		return nil, fmt.Errorf("config: jfrog.url is required")
	}
	if cfg.Source.PollInterval == 0 {
		cfg.Source.PollInterval = 60 * time.Second
	}
	if cfg.Source.CursorFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("config: resolve cursor file home dir: %w", err)
		}
		cfg.Source.CursorFile = home + "/.safedep/integration-jfrog-cursor.json"
	}
	if tok := os.Getenv("SAFEDEP_JFROG_ACCESS_TOKEN"); tok != "" {
		cfg.JFrog.AccessToken = tok
	}
	if cfg.JFrog.AccessToken == "" {
		return nil, fmt.Errorf("config: jfrog.access_token is required (or set SAFEDEP_JFROG_ACCESS_TOKEN)")
	}

	return &cfg, nil
}
```

- [ ] **Verify it compiles**

```bash
cd /home/kunalsingh/lab/safedep/cli && go build ./internal/cmd/integration/jfrog/...
```

Expected: no errors (package not yet referenced so just compiles the file).

- [ ] **Commit**

```bash
git add internal/cmd/integration/jfrog/config.go
git commit -m "feat(integration/jfrog): add config schema and loader"
```

---

## Task 2: Cursor store

**Files:**
- Create: `internal/cmd/integration/jfrog/store.go`

- [ ] **Create the cursor store**

```go
// internal/cmd/integration/jfrog/store.go
package jfrog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type cursorStore struct {
	path string
}

type cursorState struct {
	LastSeenAt time.Time `json:"last_seen_at"`
}

func newCursorStore(path string) *cursorStore {
	return &cursorStore{path: path}
}

func (s *cursorStore) Load() (time.Time, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("cursor: read: %w", err)
	}

	var state cursorState
	if err := json.Unmarshal(data, &state); err != nil {
		return time.Time{}, fmt.Errorf("cursor: parse: %w", err)
	}

	return state.LastSeenAt, nil
}

func (s *cursorStore) Save(t time.Time) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("cursor: mkdir: %w", err)
	}

	data, err := json.Marshal(cursorState{LastSeenAt: t})
	if err != nil {
		return fmt.Errorf("cursor: marshal: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("cursor: write tmp: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("cursor: rename: %w", err)
	}

	return nil
}
```

- [ ] **Verify it compiles**

```bash
cd /home/kunalsingh/lab/safedep/cli && go build ./internal/cmd/integration/jfrog/...
```

- [ ] **Commit**

```bash
git add internal/cmd/integration/jfrog/store.go
git commit -m "feat(integration/jfrog): add file-based cursor store"
```

---

## Task 3: JFrog XRay pusher

**Files:**
- Create: `internal/cmd/integration/jfrog/pusher.go`

- [ ] **Create the pusher**

```go
// internal/cmd/integration/jfrog/pusher.go
package jfrog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"github.com/safedep/dry/log"
)

type jfrogPusher struct {
	cfg    JFrogConfig
	client *http.Client
}

type jfrogEvent struct {
	ID          string           `json:"id"`
	Type        string           `json:"type"`
	Provider    string           `json:"provider"`
	PackageType string           `json:"package_type"`
	Severity    string           `json:"severity"`
	IssueKind   int              `json:"issue_kind"`
	Summary     string           `json:"summary"`
	Description string           `json:"description"`
	Properties  map[string]any   `json:"properties"`
	Components  []jfrogComponent `json:"components"`
	Sources     []jfrogSource    `json:"sources"`
}

type jfrogComponent struct {
	ID                 string   `json:"id"`
	VulnerableVersions []string `json:"vulnerable_versions"`
}

type jfrogSource struct {
	SourceID string `json:"source_id"`
}

func newJFrogPusher(cfg JFrogConfig) *jfrogPusher {
	return &jfrogPusher{
		cfg:    cfg,
		client: &http.Client{},
	}
}

func (p *jfrogPusher) Push(ctx context.Context, record *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
	pv := record.GetTarget().GetPackageVersion()
	if pv == nil {
		log.Warnf("jfrog pusher: skipping record %s: nil package version", record.GetAnalysisId())
		return nil
	}

	pkg := pv.GetPackage()
	name := pkg.GetName()
	version := pv.GetVersion()
	pkgType := ecosystemToJFrog(pkg.GetEcosystem())

	event := jfrogEvent{
		ID:          issueID(name),
		Type:        "Security",
		Provider:    "SafeDep",
		PackageType: pkgType,
		Severity:    "Critical",
		IssueKind:   1,
		Summary:     fmt.Sprintf("MALICIOUS PACKAGE: %s contains malicious code", name),
		Description: fmt.Sprintf("%s %s has been identified as a malicious package by SafeDep threat intelligence.", name, version),
		Properties:  map[string]any{},
		Components: []jfrogComponent{{
			ID:                 name,
			VulnerableVersions: []string{fmt.Sprintf("[%s]", version)},
		}},
		Sources: []jfrogSource{{SourceID: "safedep-threat-intel"}},
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("jfrog pusher: marshal: %w", err)
	}

	url := strings.TrimRight(p.cfg.URL, "/") + "/xray/api/v1/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("jfrog pusher: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("jfrog pusher: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Warnf("jfrog pusher: non-2xx for %s: status %d", event.ID, resp.StatusCode)
	}

	return nil
}

// issueID builds a JFrog-safe issue ID: prefix + truncated package name.
// Max 32 chars total; must not start with "Xray".
func issueID(name string) string {
	const prefix = "SAFEDEP-MAL-"
	max := 32 - len(prefix)
	if len(name) > max {
		name = name[:max]
	}
	return prefix + name
}

func ecosystemToJFrog(e packagev1.Ecosystem) string {
	switch e {
	case packagev1.Ecosystem_ECOSYSTEM_NPM:
		return "npm"
	case packagev1.Ecosystem_ECOSYSTEM_PYPI:
		return "pypi"
	case packagev1.Ecosystem_ECOSYSTEM_MAVEN:
		return "maven"
	case packagev1.Ecosystem_ECOSYSTEM_GO:
		return "go"
	case packagev1.Ecosystem_ECOSYSTEM_NUGET:
		return "nuget"
	case packagev1.Ecosystem_ECOSYSTEM_RUBYGEMS:
		return "gem"
	default:
		return "generic"
	}
}
```

- [ ] **Verify it compiles**

```bash
cd /home/kunalsingh/lab/safedep/cli && go build ./internal/cmd/integration/jfrog/...
```

- [ ] **Commit**

```bash
git add internal/cmd/integration/jfrog/pusher.go
git commit -m "feat(integration/jfrog): add JFrog XRay HTTP pusher"
```

---

## Task 4: gRPC poller

**Files:**
- Create: `internal/cmd/integration/jfrog/poller.go`

- [ ] **Create the poller**

```go
// internal/cmd/integration/jfrog/poller.go
package jfrog

import (
	"context"
	"fmt"
	"time"

	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type maliciousPackagePoller struct {
	svc    malysisv1grpc.MalwareAnalysisServiceClient
	cursor *cursorStore
}

func newMaliciousPackagePoller(svc malysisv1grpc.MalwareAnalysisServiceClient, cursor *cursorStore) *maliciousPackagePoller {
	return &maliciousPackagePoller{svc: svc, cursor: cursor}
}

// Poll fetches all verified malware records newer than the cursor and calls
// onRecord for each one. The cursor is advanced after each page.
func (p *maliciousPackagePoller) Poll(ctx context.Context, onRecord func(*malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error) error {
	lastSeenAt, err := p.cursor.Load()
	if err != nil {
		return fmt.Errorf("poller: load cursor: %w", err)
	}

	filter := &malysisv1.ListPackageAnalysisRecordsRequest_FilterOption{}
	filter.SetOnlyMalware(true)
	filter.SetOnlyVerified(true)

	var pageToken string
	for {
		req := &malysisv1.ListPackageAnalysisRecordsRequest{}
		if !lastSeenAt.IsZero() {
			req.SetStartFrom(timestamppb.New(lastSeenAt))
		}
		req.SetFilter(filter)

		pagination := &controltowerv1.PaginationRequest{}
		pagination.SetPageSize(100)
		if pageToken != "" {
			pagination.SetPageToken(pageToken)
		}
		req.SetPagination(pagination)

		resp, err := p.svc.ListPackageAnalysisRecords(ctx, req)
		if err != nil {
			return fmt.Errorf("poller: list records: %w", err)
		}

		var pageMaxAt time.Time
		for _, record := range resp.GetRecords() {
			if err := onRecord(record); err != nil {
				return err
			}
			if t := record.GetCreatedAt(); t != nil {
				if ts := t.AsTime(); ts.After(pageMaxAt) {
					pageMaxAt = ts
				}
			}
		}

		if !pageMaxAt.IsZero() {
			if err := p.cursor.Save(pageMaxAt); err != nil {
				return fmt.Errorf("poller: save cursor: %w", err)
			}
			lastSeenAt = pageMaxAt
		}

		nextToken := resp.GetPagination().GetNextPageToken()
		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	return nil
}
```

- [ ] **Verify it compiles**

```bash
cd /home/kunalsingh/lab/safedep/cli && go build ./internal/cmd/integration/jfrog/...
```

- [ ] **Commit**

```bash
git add internal/cmd/integration/jfrog/poller.go
git commit -m "feat(integration/jfrog): add gRPC malicious package poller"
```

---

## Task 5: FeedService

**Files:**
- Create: `internal/cmd/integration/jfrog/service.go`

- [ ] **Create the service**

```go
// internal/cmd/integration/jfrog/service.go
package jfrog

import (
	"context"
	"time"

	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"github.com/safedep/dry/log"
	drytui "github.com/safedep/dry/tui"
)

type feedService struct {
	poller *maliciousPackagePoller
	pusher *jfrogPusher
	poll   time.Duration
}

func newFeedService(svc malysisv1grpc.MalwareAnalysisServiceClient, cfg Config) *feedService {
	cursor := newCursorStore(cfg.Source.CursorFile)
	return &feedService{
		poller: newMaliciousPackagePoller(svc, cursor),
		pusher: newJFrogPusher(cfg.JFrog),
		poll:   cfg.Source.PollInterval,
	}
}

// Run polls SafeDep for verified malware and pushes to JFrog until ctx is cancelled.
func (s *feedService) Run(ctx context.Context) error {
	drytui.Info("Starting JFrog integration feed (poll interval: %s)", s.poll)

	for {
		if err := s.runOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Warnf("feed: poll cycle error: %v", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.poll):
		}
	}
}

func (s *feedService) runOnce(ctx context.Context) error {
	var pushed int
	err := s.poller.Poll(ctx, func(record *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
		if err := s.pusher.Push(ctx, record); err != nil {
			log.Warnf("feed: push %s: %v", record.GetAnalysisId(), err)
			return nil
		}
		pushed++
		return nil
	})
	if err != nil {
		return err
	}
	drytui.Info("Feed cycle complete: pushed %d records", pushed)
	return nil
}
```

- [ ] **Verify it compiles**

```bash
cd /home/kunalsingh/lab/safedep/cli && go build ./internal/cmd/integration/jfrog/...
```

- [ ] **Commit**

```bash
git add internal/cmd/integration/jfrog/service.go
git commit -m "feat(integration/jfrog): add FeedService poll-and-push loop"
```

---

## Task 6: Cobra command wiring

**Files:**
- Create: `internal/cmd/integration/jfrog/run.go`
- Create: `internal/cmd/integration/jfrog/cmd.go`
- Create: `internal/cmd/integration/cmd.go`

- [ ] **Create `run.go`**

```go
// internal/cmd/integration/jfrog/run.go
package jfrog

import (
	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

type runInput struct {
	ConfigPath string
}

func runCmd(a *app.App) *cobra.Command {
	var in runInput

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the JFrog XRay malicious package feed",
		Long:  "Poll SafeDep for verified malicious packages and push them to JFrog XRay as Custom Issues.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.DataPlane()
			if err != nil {
				return err
			}

			cfg, err := loadConfig(in.ConfigPath)
			if err != nil {
				return err
			}

			svc := newFeedService(
				malysisv1grpc.NewMalwareAnalysisServiceClient(client.Connection()),
				*cfg,
			)

			return svc.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVarP(&in.ConfigPath, "config", "c", "", "path to config file (required)")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}
```

- [ ] **Create `jfrog/cmd.go`**

```go
// internal/cmd/integration/jfrog/cmd.go
package jfrog

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "jfrog",
		Short: "JFrog XRay integration commands",
		Long:  "Commands for integrating SafeDep threat intelligence with JFrog XRay.",
	}

	cmd.AddCommand(runCmd(a))
	parent.AddCommand(cmd)
}
```

- [ ] **Create `integration/cmd.go`**

```go
// internal/cmd/integration/cmd.go
package integration

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/integration/jfrog"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "integration",
		Short: "Manage SafeDep integrations",
		Long:  "Commands for integrating SafeDep threat intelligence with third-party tools.",
	}

	jfrog.Register(cmd, a)
	root.AddCommand(cmd)
}
```

- [ ] **Verify it compiles**

```bash
cd /home/kunalsingh/lab/safedep/cli && go build ./internal/cmd/integration/...
```

- [ ] **Commit**

```bash
git add internal/cmd/integration/jfrog/run.go \
        internal/cmd/integration/jfrog/cmd.go \
        internal/cmd/integration/cmd.go
git commit -m "feat(integration/jfrog): add cobra command wiring"
```

---

## Task 7: Wire into safedep + docs

**Files:**
- Modify: `internal/cmd/safedep.go`
- Create: `docs/cmd/integration-jfrog-run.md`
- Modify: `README.md`

- [ ] **Add `integration.Register` to `internal/cmd/safedep.go`**

Current file at `internal/cmd/safedep.go`:

```go
package cmd

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/auth"
	"github.com/safedep/cli/internal/cmd/version"
	"github.com/spf13/cobra"
)

func NewSafedep(a *app.App) *cobra.Command {
	root := NewRootCommand(a)
	auth.Register(root, a)
	version.Register(root, a)
	return root
}
```

Replace with:

```go
package cmd

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cmd/auth"
	"github.com/safedep/cli/internal/cmd/integration"
	"github.com/safedep/cli/internal/cmd/version"
	"github.com/spf13/cobra"
)

func NewSafedep(a *app.App) *cobra.Command {
	root := NewRootCommand(a)
	auth.Register(root, a)
	integration.Register(root, a)
	version.Register(root, a)
	return root
}
```

- [ ] **Create `docs/cmd/integration-jfrog-run.md`**

```markdown
# safedep integration jfrog run

Poll SafeDep for verified malicious packages and push them to JFrog XRay as Custom Issues.

## Synopsis

```
safedep integration jfrog run --config <file>
```

## Flags

| Flag | Required | Description |
|---|---|---|
| `--config`, `-c` | yes | Path to YAML config file |
| `--profile` | no | Credential profile (inherited from root; defaults to `"default"`) |

## Config file

```yaml
source:
  poll_interval: 60s
  cursor_file: ~/.safedep/integration-jfrog-cursor.json

jfrog:
  url: https://company.jfrog.io
  access_token: TOKEN  # or set SAFEDEP_JFROG_ACCESS_TOKEN env var
```

## Authentication

Uses the SafeDep API key for the active profile. Run `safedep auth login` first, or set
`SAFEDEP_API_KEY` and `SAFEDEP_TENANT_ID` environment variables for headless environments.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Daemon stopped cleanly (SIGINT / SIGTERM) |
| 1 | Fatal error (config invalid, auth failed, unrecoverable poll error) |
```

- [ ] **Add link to README.md**

Find the commands table in `README.md` and add a row:

```
| `safedep integration jfrog run` | Push SafeDep malware findings to JFrog XRay | [docs](docs/cmd/integration-jfrog-run.md) |
```

If no table exists yet, add a new `## Commands` section with the link.

- [ ] **Build the full binary**

```bash
cd /home/kunalsingh/lab/safedep/cli && make build
```

Expected: `bin/safedep` produced with no errors.

- [ ] **Smoke test the command tree**

```bash
./bin/safedep integration --help
./bin/safedep integration jfrog --help
./bin/safedep integration jfrog run --help
```

Expected: all three show usage text with correct Short descriptions.

- [ ] **Commit**

```bash
git add internal/cmd/safedep.go docs/cmd/integration-jfrog-run.md README.md
git commit -m "feat(integration/jfrog): wire command into CLI and add docs"
```
