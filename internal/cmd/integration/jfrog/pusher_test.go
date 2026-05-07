package jfrog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	malysismsgv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/malysis/v1"
	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRecord builds an AnalysisRecord with the given name, version, and ecosystem.
// Centralised so tests stay focused on behaviour, not proto plumbing.
func newTestRecord(name, version string, eco packagev1.Ecosystem) *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord {
	pkg := &packagev1.Package{}
	pkg.SetName(name)
	pkg.SetEcosystem(eco)

	pv := &packagev1.PackageVersion{}
	pv.SetPackage(pkg)
	pv.SetVersion(version)

	target := &malysismsgv1.PackageAnalysisTarget{}
	target.SetPackageVersion(pv)

	rec := &malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord{}
	rec.SetAnalysisId("test-analysis-id")
	rec.SetTarget(target)
	return rec
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
	p := newJFrogPusher(JFrogConfig{URL: srv.URL, AccessToken: "TOK"})

	rec := newTestRecord("make-array", "0.1.2", packagev1.Ecosystem_ECOSYSTEM_NPM)
	status, err := p.Push(context.Background(), rec)

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
	assert.Equal(t, "SD-test-analysis-id", event.ID,
		"id is SD- prefix + the backend analysis ULID")
	assert.Equal(t, "Security", event.Type)
	assert.Equal(t, "SafeDep", event.Provider)
	assert.NotEqual(t, "JFrog", event.Provider, "provider must not be JFrog")
	assert.False(t, strings.HasPrefix(event.ID, "Xray"), "id must not start with Xray")
	assert.LessOrEqual(t, len(event.ID), 32)
	assert.Equal(t, "npm", event.PackageType)
	assert.Equal(t, "Critical", event.Severity)
	assert.Equal(t, 1, event.IssueKind, "issue_kind=1 marks it as malicious_package in XRay")

	require.Len(t, event.Components, 1)
	assert.Equal(t, "make-array", event.Components[0].ID, "component id is name only, never URI")
	require.Len(t, event.Components[0].VulnerableVersions, 1)
	assert.Equal(t, "[0.1.2]", event.Components[0].VulnerableVersions[0],
		"bracket notation required — XRay silently drops without it")

	require.Len(t, event.Sources, 1)
	assert.Equal(t, "safedep-threat-intel", event.Sources[0].SourceID)
}

func TestPush_WildcardVersion_OpenRange(t *testing.T) {
	srv, cap := newJFrogMock(t, http.StatusCreated, "")
	p := newJFrogPusher(JFrogConfig{URL: srv.URL, AccessToken: "TOK"})

	rec := newTestRecord("evil", "0", packagev1.Ecosystem_ECOSYSTEM_PYPI)
	_, err := p.Push(context.Background(), rec)
	require.NoError(t, err)

	require.Len(t, *cap, 1)
	var event jfrogEvent
	require.NoError(t, json.Unmarshal((*cap)[0].body, &event))
	// Wildcard handling now lives only in the vulnerable_versions field;
	// the ID is derived from the analysis ULID and is independent of version.
	assert.Equal(t, "(,)", event.Components[0].VulnerableVersions[0],
		"wildcard maps to open-ended XRay range")
}

func TestPush_TrimsTrailingSlashFromURL(t *testing.T) {
	srv, cap := newJFrogMock(t, http.StatusCreated, "")
	p := newJFrogPusher(JFrogConfig{URL: srv.URL + "/", AccessToken: "TOK"})

	rec := newTestRecord("foo", "1.0.0", packagev1.Ecosystem_ECOSYSTEM_NPM)
	_, err := p.Push(context.Background(), rec)
	require.NoError(t, err)

	require.Len(t, *cap, 1)
	assert.Equal(t, "/xray/api/v1/events", (*cap)[0].path,
		"trailing slash must not produce //xray/...")
}

func TestPush_NonSuccessStatus_ReturnsErrorWithBody(t *testing.T) {
	srv, _ := newJFrogMock(t, http.StatusUnauthorized, `{"error":"Bad Credentials"}`)
	p := newJFrogPusher(JFrogConfig{URL: srv.URL, AccessToken: "bad"})

	rec := newTestRecord("foo", "1.0.0", packagev1.Ecosystem_ECOSYSTEM_NPM)
	status, err := p.Push(context.Background(), rec)

	assert.Equal(t, http.StatusUnauthorized, status, "status must be returned even on error")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401", "error includes status for diagnosis")
	assert.Contains(t, err.Error(), "Bad Credentials", "error includes response body")
}

func TestPush_SkipConditions_ReturnZeroStatusNoCallNoError(t *testing.T) {
	tests := []struct {
		name    string
		makeRec func() *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord
	}{
		{
			name: "nil PackageVersion",
			makeRec: func() *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord {
				rec := &malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord{}
				rec.SetAnalysisId("nil-pv")
				rec.SetTarget(&malysismsgv1.PackageAnalysisTarget{}) // no PackageVersion
				return rec
			},
		},
		{
			name: "empty package name",
			makeRec: func() *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord {
				return newTestRecord("", "1.0.0", packagev1.Ecosystem_ECOSYSTEM_NPM)
			},
		},
		{
			name: "empty version",
			makeRec: func() *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord {
				return newTestRecord("foo", "", packagev1.Ecosystem_ECOSYSTEM_NPM)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, cap := newJFrogMock(t, http.StatusCreated, "")
			p := newJFrogPusher(JFrogConfig{URL: srv.URL, AccessToken: "TOK"})

			status, err := p.Push(context.Background(), tt.makeRec())

			require.NoError(t, err)
			assert.Equal(t, 0, status, "skip returns 0 status to signal no HTTP call made")
			assert.Empty(t, *cap, "no HTTP request must be made for skipped records")
		})
	}
}

func TestPush_LongName_PreservedInComponentId(t *testing.T) {
	srv, cap := newJFrogMock(t, http.StatusCreated, "")
	p := newJFrogPusher(JFrogConfig{URL: srv.URL, AccessToken: "TOK"})

	// The issue ID is now the backend ULID, so package name length no
	// longer matters for the ID. components[].id still keeps the full
	// name because XRay matches packages by component id.
	rec := newTestRecord("money-badger-open-rpc", "199.99.100", packagev1.Ecosystem_ECOSYSTEM_NPM)
	rec.SetAnalysisId("01KR0EKN6PMW0ZRFRN992H1PKX") // a real-shape ULID
	_, err := p.Push(context.Background(), rec)
	require.NoError(t, err)

	require.Len(t, *cap, 1)
	var event jfrogEvent
	require.NoError(t, json.Unmarshal((*cap)[0].body, &event))

	assert.Equal(t, "SD-01KR0EKN6PMW0ZRFRN992H1PKX", event.ID,
		"id is SD- prefix + backend ULID, independent of name length")
	assert.LessOrEqual(t, len(event.ID), 32, "29 chars total fits JFrog 32-char limit")
	assert.Equal(t, "money-badger-open-rpc", event.Components[0].ID,
		"component id keeps the full name; matching XRay's package identity")
}

func TestPush_EcosystemMappedToJFrogPackageType(t *testing.T) {
	cases := map[packagev1.Ecosystem]string{
		packagev1.Ecosystem_ECOSYSTEM_NPM:      "npm",
		packagev1.Ecosystem_ECOSYSTEM_PYPI:     "pypi",
		packagev1.Ecosystem_ECOSYSTEM_RUBYGEMS: "gem",
	}
	for eco, want := range cases {
		t.Run(want, func(t *testing.T) {
			srv, cap := newJFrogMock(t, http.StatusCreated, "")
			p := newJFrogPusher(JFrogConfig{URL: srv.URL, AccessToken: "TOK"})

			rec := newTestRecord("foo", "1.0.0", eco)
			_, err := p.Push(context.Background(), rec)
			require.NoError(t, err)

			var event jfrogEvent
			require.NoError(t, json.Unmarshal((*cap)[0].body, &event))
			assert.Equal(t, want, event.PackageType)
		})
	}
}

func TestIssueID(t *testing.T) {
	tests := []struct {
		name       string
		analysisID string
		want       string
	}{
		{
			name:       "ULID is prefixed with SD-",
			analysisID: "01KR0EKN6PMW0ZRFRN992H1PKX",
			want:       "SD-01KR0EKN6PMW0ZRFRN992H1PKX",
		},
		{
			name:       "second ULID gets a different ID",
			analysisID: "01KR0F5ZQ3J8Y2WBHPD7XKMVNT",
			want:       "SD-01KR0F5ZQ3J8Y2WBHPD7XKMVNT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := issueID(tt.analysisID)

			assert.Equal(t, tt.want, got)

			// JFrog constraints: <=32 chars, must not start with "Xray",
			// must not be "JFrog". The "SD-" prefix + 26-char ULID gives
			// us 29 chars with plenty of headroom.
			assert.LessOrEqual(t, len(got), 32, "issue ID exceeds JFrog 32-char limit")
			assert.False(t, strings.HasPrefix(got, "Xray"), "issue ID must not start with Xray")
			assert.NotEqual(t, "JFrog", got, "issue ID must not be JFrog")
		})
	}
}

func TestVulnerableVersions(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name:    "exact version wrapped in brackets",
			version: "1.0.0",
			want:    "[1.0.0]",
		},
		{
			// Without the wildcard mapping XRay would silently drop the
			// record - the symptom that motivated the (,) rule in the
			// docs/jfrog-integration/windcard-version.md note.
			name:    "wildcard 0 mapped to open range",
			version: "0",
			want:    "(,)",
		},
		{
			name:    "pre-release version preserved",
			version: "1.0.0-beta.1",
			want:    "[1.0.0-beta.1]",
		},
		{
			name:    "version with build metadata preserved",
			version: "1.0.0+sha.abc",
			want:    "[1.0.0+sha.abc]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vulnerableVersions(tt.version)
			assert.Equal(t, tt.want, got)
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
