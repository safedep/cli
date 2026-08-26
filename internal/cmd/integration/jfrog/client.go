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
)

// xrayClient is the port the feed service pushes through. jfrogClient is the
// real adapter (HTTP to XRay); printClient (printclient.go) is the dry-run
// adapter that prints what would be pushed and sends nothing. Swapping the
// adapter is the only difference between a real run and a dry-run.
type xrayClient interface {
	validate(ctx context.Context) error
	pushMaliciousPackage(ctx context.Context, report *threatintelv1.PackageReport) (string, int, error)
	deleteMaliciousPackage(ctx context.Context, report *threatintelv1.PackageReport) (string, int, error)
}

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
	rep  *reporter
}

var _ xrayClient = (*jfrogClient)(nil)

func newJFrogClient(cfg jfrogConfig, rep *reporter) *jfrogClient {
	return &jfrogClient{
		cfg:  cfg,
		http: &http.Client{Timeout: httpTimeout},
		rep:  rep,
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

	// maxIssueIDLen is JFrog's id limit. It silently drops longer ids, and
	// report_id is only bounded at 128 chars, so buildEvent skips over-length
	// ids rather than push events that never land.
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

// issueID is a pure function of the permanent report_id: no randomness, no
// truncation. Delete and Stage 3 update rely on reconstructing the exact id
// that was pushed, since XRay has no lookup by name. It is a package function,
// not a method, so both the real and print clients share one definition.
func issueID(report *threatintelv1.PackageReport) string {
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
	c.rep.logInfo("Validating JFrog connectivity")
	status, body, err := c.do(ctx, http.MethodGet, policiesPath, nil)
	if err != nil {
		return fmt.Errorf("jfrog validate: cannot reach %s: %w", c.cfg.url, err)
	}

	switch status {
	case http.StatusOK:
		c.rep.logSuccess("JFrog connectivity OK (URL + token verified)")
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

// pushMaliciousPackage submits an XRay Custom Issue for the report. It returns
// the issue id, the HTTP status, and an error on transport or non-2xx. A
// skipped report (see buildEvent) returns ("", 0, nil). A 400 "already exists"
// is benign, mirroring a delete 404: the issue is already present, which is the
// desired state, so it returns (id, 400, nil) with no error.
func (c *jfrogClient) pushMaliciousPackage(ctx context.Context, report *threatintelv1.PackageReport) (string, int, error) {
	event, reason, ok := buildEvent(report)
	if !ok {
		logSkip(c.rep, report, reason)
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
	if status == http.StatusBadRequest && isAlreadyExists(respBody) {
		// XRay does not upsert on a duplicate id, so a re-push of an unchanged
		// report returns 400 "already exists". The issue is already present, so
		// this is a benign no-op, not a failure.
		return event.ID, status, nil
	}
	if status < 200 || status >= 300 {
		return event.ID, status, fmt.Errorf("jfrog client: %s: status %d: %s", event.ID, status, string(respBody))
	}
	return event.ID, status, nil
}

// isAlreadyExists reports whether an XRay error body signals that the Custom
// Issue id is already present. XRay returns 400 with a body like
// {"error":"Vulnerability already exists"} on a duplicate id.
func isAlreadyExists(respBody []byte) bool {
	return strings.Contains(strings.ToLower(string(respBody)), "already exists")
}

// deleteMaliciousPackage deletes the XRay Custom Issue for a withdrawn report.
// It returns the issue id, the HTTP status, and an error on transport or an
// unexpected non-2xx. An id that would have been too long to push is skipped
// (return "", 0, nil), and a 404 is benign: the issue is already absent, which
// is the state a delete aims for.
func (c *jfrogClient) deleteMaliciousPackage(ctx context.Context, report *threatintelv1.PackageReport) (string, int, error) {
	id := issueID(report)
	if len(id) > maxIssueIDLen {
		// Never pushed (see buildEvent), so nothing to delete.
		return "", 0, nil
	}

	status, respBody, err := c.do(ctx, http.MethodDelete, eventsPath+"/"+id, nil)
	if err != nil {
		return id, 0, fmt.Errorf("jfrog client: http: %w", err)
	}
	if status == http.StatusNotFound {
		return id, status, nil
	}
	if status < 200 || status >= 300 {
		return id, status, fmt.Errorf("jfrog client: delete %s: status %d: %s", id, status, string(respBody))
	}
	return id, status, nil
}

// buildEvent builds the XRay payload, or returns ok=false with a skip reason.
// It is pure (no logging) so both clients build the identical preview and each
// reports the skip through its own emitter. Skip rules live here, beside the
// wire format. Note: ecosystem is on the report, not the package, and empty
// versions (all versions) are valid, not a skip.
func buildEvent(report *threatintelv1.PackageReport) (jfrogEvent, string, bool) {
	pkg := report.GetPackage()
	name := pkg.GetName()
	if name == "" {
		return jfrogEvent{}, "missing package name", false
	}

	// JFrog silently drops an event whose id is too long, so skip it. Skip
	// rather than truncate: the id must stay a pure function of report_id so
	// delete and Stage 3 update can reconstruct it.
	id := issueID(report)
	if len(id) > maxIssueIDLen {
		return jfrogEvent{}, fmt.Sprintf("issue id %q exceeds JFrog %d-char limit", id, maxIssueIDLen), false
	}

	// XRay summary is a synthesized headline, not the feed's title. The feed
	// title is only the first few words of the feed summary, so it is a poor
	// standalone headline. The XRay description carries the feed's full summary.
	summary := fmt.Sprintf("%s identified as Malware by SafeDep", name)
	description := report.GetSummary()
	if description == "" {
		description = fmt.Sprintf("%s has been identified as a malicious package by SafeDep threat intelligence.", name)
	}

	return jfrogEvent{
		ID:          id,
		Type:        "Security",
		Provider:    "SafeDep",
		PackageType: ecosystemToJFrog(report.GetEcosystem()),
		Severity:    "Critical",
		IssueKind:   1,
		Summary:     summary,
		Description: description,
		Properties:  map[string]any{},
		Components: []jfrogComponent{{
			ID:                 name,
			VulnerableVersions: vulnerableVersionRanges(pkg.GetVersions()),
		}},
		Sources: []jfrogSource{{SourceID: "safedep-threat-intel"}},
	}, "", true
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

// vulnerableVersionRanges renders affected versions in XRay's bracket notation
// for one component. Empty entries are dropped ("[]" is silently dropped by
// XRay), and an empty result means all versions: "(,)".
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
