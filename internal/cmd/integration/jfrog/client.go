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
	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	"github.com/safedep/dry/log"
	drytui "github.com/safedep/dry/tui"
)

// jfrogClient is the single source of truth for JFrog XRay protocol
// details: HTTP endpoints, authentication, payload format, issue ID
// rules, version range notation, and ecosystem mapping.
//
// Other files in this package compose this client; they must not encode
// JFrog rules themselves. If JFrog adds a new endpoint, changes the
// payload, or relaxes the ID limit, this is the only file to touch.
type jfrogClient struct {
	cfg  jfrogConfig
	http *http.Client
}

func newJFrogClient(cfg jfrogConfig) *jfrogClient {
	return &jfrogClient{
		cfg:  cfg,
		http: &http.Client{Timeout: httpTimeout},
	}
}

const (
	// httpTimeout caps the total time of a single XRay request including
	// dial, TLS, headers, and body. Without it the daemon would hang on
	// an unresponsive JFrog instance.
	httpTimeout = 30 * time.Second

	// maxRespBody bounds how much of the XRay response body we read.
	// Bodies are only used for diagnostics on non-2xx responses; 1 MiB is
	// plenty and caps worst-case memory if a misbehaving proxy returns an
	// unbounded stream.
	maxRespBody = 1 << 20

	// userAgent identifies this integration on the wire so JFrog operators
	// can recognise our traffic in access logs.
	userAgent = "safedep-cli/integration-jfrog"

	// XRay paths. Centralised so URL construction is never duplicated.
	eventsPath   = "/xray/api/v1/events"
	policiesPath = "/xray/api/v1/policies"

	// issueIDPrefix is prepended to the SafeDep report id to produce the
	// XRay Custom Issue id. The prefix also satisfies JFrog's id rules:
	// it must not start with "Xray" and must not be literally "JFrog".
	issueIDPrefix = "SD-"

	// maxIssueIDLen is JFrog's hard limit on a Custom Issue id. JFrog
	// SILENTLY drops any event whose id exceeds this, so buildEvent skips
	// an over-length id rather than pushing an event that never lands.
	// report_id is only contractually <= 128 chars (not a guaranteed
	// 26-char ULID), so "SD-" + report_id can exceed the limit.
	maxIssueIDLen = 32
)

// XRay Custom Issue wire format. Field tags must match what JFrog accepts;
// any change here changes what every PushMaliciousPackage call sends.
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

// issueID returns the XRay Custom Issue id for the supplied report.
//
// It is a pure, reproducible function of report_id: no randomness, no
// timestamps, no truncation, no hashing. This is a hard requirement for
// later stages. Delete and update have no way to look an issue up by
// name, so they reconstruct the exact id that was pushed. report_id is
// permanent across the report lifecycle (verification, late indicators,
// and withdrawal are updates to the same id), so a re-delivery always
// carries the id originally pushed.
func (c *jfrogClient) issueID(report *threatintelv1.PackageReport) string {
	return issueIDPrefix + report.GetReportId()
}

// Validate performs a pre-flight check that proves three things in a
// single round trip: the URL is reachable, the access token is valid,
// and the token has XRay read permissions.
//
// We probe GET /xray/api/v1/policies (authenticated, read-only) instead
// of /system/version, because system/version returns 200 even without an
// auth header and would silently pass with a wrong token.
//
// Status code to error mapping:
//   - 200          : URL + token + permissions all OK
//   - 401          : token invalid or expired
//   - 403          : token valid but lacks XRay read permission
//   - 404          : URL points somewhere that is not an XRay instance
//   - other / net  : surfaced verbatim with the response body for diagnosis
func (c *jfrogClient) validate(ctx context.Context) error {
	status, body, err := c.do(ctx, http.MethodGet, policiesPath, nil)
	if err != nil {
		return fmt.Errorf("jfrog validate: cannot reach %s: %w", c.cfg.url, err)
	}

	switch status {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("jfrog validate: 401 Unauthorized - access token is invalid or expired")
	case http.StatusForbidden:
		return fmt.Errorf("jfrog validate: 403 Forbidden - token lacks XRay read permission")
	case http.StatusNotFound:
		return fmt.Errorf("jfrog validate: 404 Not Found - %s does not appear to be an XRay endpoint", c.cfg.url)
	default:
		return fmt.Errorf("jfrog validate: unexpected status %d: %s", status, string(body))
	}
}

// pushMaliciousPackage submits an XRay Custom Issue for the supplied
// malicious package report.
//
// Returns:
//   - issueID: the XRay Custom Issue id constructed for this report.
//     Empty when the report is skipped before any HTTP call.
//   - status: HTTP status code from JFrog (0 when skipped pre-call).
//   - err: non-nil on transport failure, build error, or non-2xx response.
//
// Reports are skipped (and ("", 0, nil) returned) when the report has no
// package, an empty name, or an over-length issue id. See buildEvent for
// the skip rules. Pushing such a report would produce a payload XRay
// silently drops.
func (c *jfrogClient) pushMaliciousPackage(ctx context.Context, report *threatintelv1.PackageReport) (string, int, error) {
	event, ok := c.buildEvent(report)
	if !ok {
		return "", 0, nil
	}

	body, err := json.Marshal(event)
	if err != nil {
		return event.ID, 0, fmt.Errorf("jfrog client: marshal: %w", err)
	}

	status, respBody, err := c.do(ctx, http.MethodPost, eventsPath, body)
	if err != nil {
		return event.ID, 0, fmt.Errorf("jfrog client: http: %w", err)
	}
	if status < 200 || status >= 300 {
		return event.ID, status, fmt.Errorf("jfrog client: %s: status %d: %s", event.ID, status, string(respBody))
	}
	return event.ID, status, nil
}

// buildEvent constructs the XRay payload and reports whether the report
// has all the fields XRay requires. Skip rules live here so the wire
// format and the skip contract stay co-located.
//
// Ecosystem lives on the report, not the package. Empty versions are
// valid (they mean every version is affected) and are NOT a skip.
// Withdrawn reports are NOT skipped here either: the source delivers
// them and feedService decides what to do (Stage 1: a logged no-op).
func (c *jfrogClient) buildEvent(report *threatintelv1.PackageReport) (jfrogEvent, bool) {
	pkg := report.GetPackage()
	name := pkg.GetName()
	if name == "" {
		drytui.Warning("Skipping report %s: missing package name", report.GetReportId())
		return jfrogEvent{}, false
	}

	// JFrog silently drops any event whose id exceeds maxIssueIDLen. Skip
	// rather than push a lost event. The guard skips, it does not
	// truncate: a truncated id would break reproducibility for delete and
	// update, and an id too long to push is also too long to delete.
	id := c.issueID(report)
	if len(id) > maxIssueIDLen {
		drytui.Warning("Skipping report %s: issue id %q exceeds JFrog %d-char limit", report.GetReportId(), id, maxIssueIDLen)
		return jfrogEvent{}, false
	}

	return jfrogEvent{
		ID:          id,
		Type:        "Security",
		Provider:    "SafeDep",
		PackageType: ecosystemToJFrog(report.GetEcosystem()),
		Severity:    "Critical",
		IssueKind:   1,
		Summary:     fmt.Sprintf("MALICIOUS PACKAGE: %s contains malicious code", name),
		Description: fmt.Sprintf("%s has been identified as a malicious package by SafeDep threat intelligence.", name),
		Properties:  map[string]any{},
		Components: []jfrogComponent{{
			ID:                 name,
			VulnerableVersions: vulnerableVersionRanges(pkg.GetVersions()),
		}},
		Sources: []jfrogSource{{SourceID: "safedep-threat-intel"}},
	}, true
}

// do issues a single XRay request with the standard headers and bounded
// body read. Returns (status, responseBody, error) so callers can map
// status codes without reaching for *http.Response themselves.
func (c *jfrogClient) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	url := strings.TrimRight(c.cfg.url, "/") + path

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.accessToken)
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Internal diagnostic per AGENTS.md: deferred cleanup
			// failures are not actionable by the operator.
			log.Warnf("jfrog client: close response body: %v", err)
		}
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBody))
	if err != nil {
		log.Warnf("jfrog client: read response body: %v", err)
	}
	return resp.StatusCode, respBody, nil
}

// vulnerableVersionRanges maps a report's affected versions to JFrog
// XRay's range notation for a single component. XRay requires bracket
// notation for an exact version and "(,)" for an open-ended range.
// Without brackets XRay silently drops the event.
//
// A report may carry zero, one, or many exact versions. An empty slice
// means every version is affected. Empty-string entries are dropped: the
// feed bounds each version to <= 256 chars but has no min length, and
// "[]" is silently dropped by XRay, so an empty entry must not slip into
// an otherwise valid list. When nothing remains, the result is the
// open-ended "(,)" (all versions).
func vulnerableVersionRanges(versions []string) []string {
	ranges := make([]string, 0, len(versions))
	for _, v := range versions {
		if v == "" {
			continue
		}
		ranges = append(ranges, "["+v+"]")
	}
	if len(ranges) == 0 {
		return []string{"(,)"}
	}
	return ranges
}

// ecosystemToJFrog maps a SafeDep ecosystem enum to the JFrog XRay
// package_type string. Unmapped ecosystems fall back to "generic" so
// new SafeDep enums never panic the pusher.
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
	case packagev1.Ecosystem_ECOSYSTEM_CARGO:
		return "cargo"
	default:
		return "generic"
	}
}
