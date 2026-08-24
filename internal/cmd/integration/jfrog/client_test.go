package jfrog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestReport builds a PackageReport with the given report id, name,
// ecosystem, and affected versions. Centralised so tests stay focused on
// behaviour, not proto plumbing.
func newTestReport(reportID, name string, eco packagev1.Ecosystem, versions ...string) *threatintelv1.PackageReport {
	pkg := &threatintelv1.ReportPackage{}
	pkg.SetName(name)
	pkg.SetVersions(versions)

	report := &threatintelv1.PackageReport{}
	report.SetReportId(reportID)
	report.SetEcosystem(eco)
	report.SetVerdict(threatintelv1.ThreatVerdict_THREAT_VERDICT_MALICIOUS)
	report.SetPackage(pkg)
	return report
}

// captured holds what the JFrog mock server received so tests can assert on it.
type captured struct {
	method  string
	path    string
	headers http.Header
	body    []byte
}

// newJFrogMock returns an httptest server that records each request and
// responds with the supplied status code and response body. Callers use
// the captured slice to assert on the request shape.
func newJFrogMock(t *testing.T, status int, respBody string) (*httptest.Server, *[]captured) {
	t.Helper()
	cap := &[]captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*cap = append(*cap, captured{
			method:  r.Method,
			path:    r.URL.Path,
			headers: r.Header.Clone(),
			body:    body,
		})
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func TestPush_HappyPath_ConstructsCorrectRequest(t *testing.T) {
	srv, cap := newJFrogMock(t, http.StatusCreated, "")
	c := newJFrogClient(jfrogConfig{url: srv.URL, accessToken: "TOK"})

	report := newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "make-array", packagev1.Ecosystem_ECOSYSTEM_NPM, "0.1.2")
	_, status, err := c.pushMaliciousPackage(context.Background(), report)

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, status)
	require.Len(t, *cap, 1)

	got := (*cap)[0]
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "/xray/api/v1/events", got.path)
	assert.Equal(t, "Bearer TOK", got.headers.Get("Authorization"))
	assert.Equal(t, "application/json", got.headers.Get("Content-Type"))
	assert.Equal(t, userAgent, got.headers.Get("User-Agent"))

	// Decode and assert the wire format matches the JFrog reference exactly.
	// These are the constraints that silently break delivery if wrong.
	var event jfrogEvent
	require.NoError(t, json.Unmarshal(got.body, &event))
	assert.Equal(t, "SD-01KR0EKN6PMW0ZRFRN992H1PKX", event.ID,
		"id is SD- prefix + the report id")
	assert.Equal(t, "Security", event.Type)
	assert.Equal(t, "SafeDep", event.Provider)
	assert.NotEqual(t, "JFrog", event.Provider, "provider must not be JFrog")
	assert.False(t, strings.HasPrefix(event.ID, "Xray"), "id must not start with Xray")
	assert.LessOrEqual(t, len(event.ID), maxIssueIDLen)
	assert.Equal(t, "npm", event.PackageType)
	assert.Equal(t, "Critical", event.Severity)
	assert.Equal(t, 1, event.IssueKind, "issue_kind=1 marks it as malicious_package in XRay")

	require.Len(t, event.Components, 1)
	assert.Equal(t, "make-array", event.Components[0].ID, "component id is name only, never URI")
	require.Len(t, event.Components[0].VulnerableVersions, 1)
	assert.Equal(t, "[0.1.2]", event.Components[0].VulnerableVersions[0],
		"bracket notation required - XRay silently drops without it")

	require.Len(t, event.Sources, 1)
	assert.Equal(t, "safedep-threat-intel", event.Sources[0].SourceID)
}

func TestPush_MultipleVersions_OneComponentManyRanges(t *testing.T) {
	srv, cap := newJFrogMock(t, http.StatusCreated, "")
	c := newJFrogClient(jfrogConfig{url: srv.URL, accessToken: "TOK"})

	report := newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "express-logger-pro",
		packagev1.Ecosystem_ECOSYSTEM_NPM, "9.9.9", "9.9.10", "2.0.0")
	_, _, err := c.pushMaliciousPackage(context.Background(), report)
	require.NoError(t, err)

	require.Len(t, *cap, 1)
	var event jfrogEvent
	require.NoError(t, json.Unmarshal((*cap)[0].body, &event))

	// One report is one issue is one component regardless of version count.
	require.Len(t, event.Components, 1)
	assert.Equal(t, "express-logger-pro", event.Components[0].ID)
	assert.Equal(t, []string{"[9.9.9]", "[9.9.10]", "[2.0.0]"}, event.Components[0].VulnerableVersions,
		"every affected version maps into one component's ranges")
}

func TestPush_EmptyVersions_OpenRange(t *testing.T) {
	srv, cap := newJFrogMock(t, http.StatusCreated, "")
	c := newJFrogClient(jfrogConfig{url: srv.URL, accessToken: "TOK"})

	// Empty versions means every version is affected. It is NOT skipped.
	report := newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "evil", packagev1.Ecosystem_ECOSYSTEM_PYPI)
	_, status, err := c.pushMaliciousPackage(context.Background(), report)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, status, "empty versions is valid, not a skip")

	require.Len(t, *cap, 1)
	var event jfrogEvent
	require.NoError(t, json.Unmarshal((*cap)[0].body, &event))
	require.Len(t, event.Components[0].VulnerableVersions, 1)
	assert.Equal(t, "(,)", event.Components[0].VulnerableVersions[0],
		"empty versions maps to the open-ended XRay range")
}

func TestPush_TrimsTrailingSlashFromURL(t *testing.T) {
	srv, cap := newJFrogMock(t, http.StatusCreated, "")
	c := newJFrogClient(jfrogConfig{url: srv.URL + "/", accessToken: "TOK"})

	report := newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "foo", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
	_, _, err := c.pushMaliciousPackage(context.Background(), report)
	require.NoError(t, err)

	require.Len(t, *cap, 1)
	assert.Equal(t, "/xray/api/v1/events", (*cap)[0].path,
		"trailing slash must not produce //xray/...")
}

func TestPush_NonSuccessStatus_ReturnsErrorWithBody(t *testing.T) {
	srv, _ := newJFrogMock(t, http.StatusUnauthorized, `{"error":"Bad Credentials"}`)
	c := newJFrogClient(jfrogConfig{url: srv.URL, accessToken: "bad"})

	report := newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "foo", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
	_, status, err := c.pushMaliciousPackage(context.Background(), report)

	assert.Equal(t, http.StatusUnauthorized, status, "status must be returned even on error")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401", "error includes status for diagnosis")
	assert.Contains(t, err.Error(), "Bad Credentials", "error includes response body")
}

func TestPush_SkipConditions_ReturnZeroStatusNoCallNoError(t *testing.T) {
	// An id of "SD-" + a 30-char report id is 33 chars, one over the limit.
	overLengthReportID := strings.Repeat("A", 30)

	tests := []struct {
		name       string
		makeReport func() *threatintelv1.PackageReport
	}{
		{
			name: "nil package",
			makeReport: func() *threatintelv1.PackageReport {
				report := &threatintelv1.PackageReport{}
				report.SetReportId("01KR0EKN6PMW0ZRFRN992H1PKX")
				return report
			},
		},
		{
			name: "empty package name",
			makeReport: func() *threatintelv1.PackageReport {
				return newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
			},
		},
		{
			name: "over-length issue id",
			makeReport: func() *threatintelv1.PackageReport {
				return newTestReport(overLengthReportID, "foo", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, cap := newJFrogMock(t, http.StatusCreated, "")
			c := newJFrogClient(jfrogConfig{url: srv.URL, accessToken: "TOK"})

			_, status, err := c.pushMaliciousPackage(context.Background(), tt.makeReport())

			require.NoError(t, err)
			assert.Equal(t, 0, status, "skip returns 0 status to signal no HTTP call made")
			assert.Empty(t, *cap, "no HTTP request must be made for skipped reports")
		})
	}
}

func TestClient_IssueID_ReproducibleAndGuarded(t *testing.T) {
	c := newJFrogClient(jfrogConfig{url: "https://example.jfrog.io", accessToken: "tok"})

	t.Run("SD- prefix and pure function of report id", func(t *testing.T) {
		report := newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "foo", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")

		id := c.issueID(report)
		assert.Equal(t, "SD-01KR0EKN6PMW0ZRFRN992H1PKX", id)

		// Reproducibility: the same report must yield the same id every
		// call. Stage 2 delete and Stage 3 update rely on this to
		// reconstruct the pushed id with no stored name-to-id mapping.
		assert.Equal(t, id, c.issueID(report), "issue id must be a pure function of the report")

		assert.LessOrEqual(t, len(id), maxIssueIDLen, "issue id must fit the JFrog limit")
		assert.False(t, strings.HasPrefix(id, "Xray"), "issue id must not start with Xray")
		assert.NotEqual(t, "JFrog", id, "issue id must not be JFrog")
	})

	t.Run("distinct report ids yield distinct issue ids", func(t *testing.T) {
		a := newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "foo", packagev1.Ecosystem_ECOSYSTEM_NPM)
		b := newTestReport("01KR0F5ZQ3J8Y2WBHPD7XKMVNT", "foo", packagev1.Ecosystem_ECOSYSTEM_NPM)
		assert.NotEqual(t, c.issueID(a), c.issueID(b))
	})
}

func TestVulnerableVersionRanges(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     []string
	}{
		{
			name:     "empty means all versions",
			versions: nil,
			want:     []string{"(,)"},
		},
		{
			name:     "single exact version wrapped in brackets",
			versions: []string{"1.0.0"},
			want:     []string{"[1.0.0]"},
		},
		{
			name:     "many versions map into one range list",
			versions: []string{"1.0.0", "1.0.1", "2.0.0"},
			want:     []string{"[1.0.0]", "[1.0.1]", "[2.0.0]"},
		},
		{
			name:     "empty string entries dropped",
			versions: []string{"", "1.0.0", ""},
			want:     []string{"[1.0.0]"},
		},
		{
			name:     "all-empty entries fall back to all versions",
			versions: []string{"", ""},
			want:     []string{"(,)"},
		},
		{
			name:     "pre-release and build metadata preserved",
			versions: []string{"1.0.0-beta.1", "1.0.0+sha.abc"},
			want:     []string{"[1.0.0-beta.1]", "[1.0.0+sha.abc]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, vulnerableVersionRanges(tt.versions))
		})
	}
}

func TestEcosystemToJFrog(t *testing.T) {
	tests := []struct {
		ecosystem packagev1.Ecosystem
		want      string
	}{
		{packagev1.Ecosystem_ECOSYSTEM_NPM, "npm"},
		{packagev1.Ecosystem_ECOSYSTEM_PYPI, "pypi"},
		{packagev1.Ecosystem_ECOSYSTEM_MAVEN, "maven"},
		{packagev1.Ecosystem_ECOSYSTEM_GO, "go"},
		{packagev1.Ecosystem_ECOSYSTEM_NUGET, "nuget"},
		// rubygems uses the JFrog naming "gem", not "rubygems".
		{packagev1.Ecosystem_ECOSYSTEM_RUBYGEMS, "gem"},
		// Unmapped or unknown ecosystems fall back to "generic" so the
		// pusher does not panic on a new SafeDep ecosystem enum.
		{packagev1.Ecosystem_ECOSYSTEM_UNSPECIFIED, "generic"},
		{packagev1.Ecosystem_ECOSYSTEM_CARGO, "cargo"},
		{packagev1.Ecosystem_ECOSYSTEM_GITHUB_ACTIONS, "generic"},
	}

	for _, tt := range tests {
		t.Run(tt.ecosystem.String(), func(t *testing.T) {
			got := ecosystemToJFrog(tt.ecosystem)
			assert.Equal(t, tt.want, got)
		})
	}
}
