// internal/cmd/integration/jfrog/pusher.go
package jfrog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"github.com/safedep/dry/log"
)

const (
	// httpTimeout caps the total time of a single XRay request including dial,
	// TLS, headers and body. Without it the daemon would hang on an
	// unresponsive JFrog instance.
	httpTimeout = 30 * time.Second

	// maxRespBody limits how much of the XRay response body we read into
	// memory. The body is only used for diagnostics on non-2xx responses, so
	// 1 MiB is plenty and bounds worst-case memory if a misbehaving proxy
	// returns an unbounded stream.
	maxRespBody = 1 << 20

	// userAgent identifies the integration on the wire so JFrog operators can
	// distinguish our traffic in access logs.
	userAgent = "safedep-cli/integration-jfrog"
)

// jfrogPusher converts a SafeDep malware analysis record into a JFrog XRay
// custom issue and POSTs it to the configured XRay instance.
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
		client: &http.Client{Timeout: httpTimeout},
	}
}

// Push sends the record to JFrog XRay and returns the HTTP status code so the
// caller can log it alongside package context.
func (p *jfrogPusher) Push(ctx context.Context, record *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) (int, error) {
	pv := record.GetTarget().GetPackageVersion()
	if pv == nil {
		log.Warnf("jfrog pusher: skipping record %s: nil package version", record.GetAnalysisId())
		return 0, nil
	}

	pkg := pv.GetPackage()
	name := pkg.GetName()
	version := pv.GetVersion()
	if name == "" {
		log.Warnf("jfrog pusher: skipping record %s: empty package name", record.GetAnalysisId())
		return 0, nil
	}
	// Empty version would render as "[]" in XRay's range notation, which the
	// API silently drops. Refuse rather than push a record that will not flag.
	if version == "" {
		log.Warnf("jfrog pusher: skipping record %s: empty version", record.GetAnalysisId())
		return 0, nil
	}
	pkgType := ecosystemToJFrog(pkg.GetEcosystem())

	event := jfrogEvent{
		ID:          issueID(name, version),
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
			VulnerableVersions: []string{vulnerableVersions(version)},
		}},
		Sources: []jfrogSource{{SourceID: "safedep-threat-intel"}},
	}

	body, err := json.Marshal(event)
	if err != nil {
		return 0, fmt.Errorf("jfrog pusher: marshal: %w", err)
	}

	url := strings.TrimRight(p.cfg.URL, "/") + "/xray/api/v1/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("jfrog pusher: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("jfrog pusher: http: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warnf("jfrog pusher: close response body: %v", err)
		}
	}()

	// Bounded read: the body is only used for diagnostics, never trusted.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBody))
	if err != nil {
		log.Warnf("jfrog pusher: read response body: %v", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("jfrog pusher: %s: status %d: %s", event.ID, resp.StatusCode, string(respBody))
	}

	return resp.StatusCode, nil
}

// issueID builds a JFrog XRay custom issue ID from package name and version.
//
// JFrog constraints: max 32 chars, must not start with "Xray", must not be "JFrog".
//
// Budget: "SD-MAL-" (7) + name (13) + "-" (1) + version (11) = 32.
// Both name and version are independently truncated so the version is never
// silently dropped. This matters for scoped packages like @company/pkg where
// the name alone can exhaust the budget, making multiple versions
// indistinguishable in XRay.
//
// Trailing hyphens left by truncation (e.g. "money-badger-open-rpc"[:13]
// -> "money-badger-") are trimmed so the ID does not contain "--".
//
// When version is "0" (SafeDep wildcard for all versions), the version segment
// is "ALL" so the ID is visibly distinct from any real version.
func issueID(name, version string) string {
	const prefix = "SD-MAL-"
	const nameBudget = 13
	const verBudget = 11
	if version == "0" {
		version = "ALL"
	}
	if len(name) > nameBudget {
		name = name[:nameBudget]
	}
	if len(version) > verBudget {
		version = version[:verBudget]
	}
	name = strings.TrimRight(name, "-")
	version = strings.TrimLeft(version, "-")
	return prefix + name + "-" + version
}

// vulnerableVersions maps a SafeDep version string to the JFrog XRay range notation.
//
// SafeDep backend sends version "0" as a wildcard meaning all versions are malicious.
// JFrog requires bracket notation for exact versions and open-ended range "(,)" for all versions:
//   - version "0"    → "(,)"     matches every version of the package
//   - any other ver  → "[1.0.4]" matches that exact version only
//
// Without brackets, XRay silently drops the record and nothing is flagged.
func vulnerableVersions(version string) string {
	if version == "0" {
		return "(,)"
	}
	return "[" + version + "]"
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
