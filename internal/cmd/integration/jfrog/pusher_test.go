package jfrog

import (
	"strings"
	"testing"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	"github.com/stretchr/testify/assert"
)

func TestIssueID(t *testing.T) {
	tests := []struct {
		name    string
		pkgName string
		version string
		want    string
	}{
		{
			name:    "short name and version fit",
			pkgName: "foo",
			version: "1.0.0",
			want:    "SD-MAL-foo-1.0.0",
		},
		{
			name:    "wildcard version 0 becomes ALL",
			pkgName: "foo",
			version: "0",
			want:    "SD-MAL-foo-ALL",
		},
		{
			name:    "long package name truncated to 13 chars",
			pkgName: "very-long-package-name",
			version: "1.0.0",
			want:    "SD-MAL-very-long-pac-1.0.0",
		},
		{
			name:    "long version truncated to 11 chars",
			pkgName: "foo",
			version: "1.0.0-beta.1.2.3",
			want:    "SD-MAL-foo-1.0.0-beta.",
		},
		{
			// Regression for the money-badger-open-rpc bug: truncating the
			// name to 13 chars left a trailing hyphen, producing
			// SD-MAL-money-badger--199.99.100 (double hyphen). The fix
			// trims trailing hyphens after truncation.
			name:    "trailing hyphen from truncation is trimmed",
			pkgName: "money-badger-open-rpc",
			version: "199.99.100",
			want:    "SD-MAL-money-badger-199.99.100",
		},
		{
			name:    "scoped package fits within budget",
			pkgName: "@company/pkg",
			version: "1.0.0",
			want:    "SD-MAL-@company/pkg-1.0.0",
		},
		{
			name:    "long scoped package truncated cleanly",
			pkgName: "@company/very-long",
			version: "1.0.0",
			want:    "SD-MAL-@company/very-1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := issueID(tt.pkgName, tt.version)

			assert.Equal(t, tt.want, got)

			// JFrog constraints: <=32 chars, must not start with "Xray".
			// Guard the invariants explicitly so a future change cannot
			// silently violate them.
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
		{packagev1.Ecosystem_ECOSYSTEM_CARGO, "generic"},
	}

	for _, tt := range tests {
		t.Run(tt.ecosystem.String(), func(t *testing.T) {
			got := ecosystemToJFrog(tt.ecosystem)
			assert.Equal(t, tt.want, got)
		})
	}
}
