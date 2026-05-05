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
