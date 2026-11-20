package cloudpolicy

import (
	"testing"
	"time"

	optionsv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/options/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestFields(t *testing.T) {
	got := Fields(specWith(scangatev1.Severity_SEVERITY_MEDIUM, 168*time.Hour))
	assert.Equal(t, []FieldValue{
		{Path: "package_admission.cooldown.min_age", Value: "168h", Human: "7d"},
		{Path: "scan_gate.fail_on", Value: "medium", Human: "medium"},
	}, got)
	assert.Empty(t, Fields(nil))
}

func TestFields_ListSettings(t *testing.T) {
	licenses := &scangatev1.LicenseGate{}
	deny := &scangatev1.LicenseList{}
	deny.SetLicenses([]string{"GPL-3.0-only", "AGPL-3.0-only"})
	licenses.SetDeny(deny)
	licenses.SetAllow(&scangatev1.LicenseList{})
	spec := specWith(scangatev1.Severity_SEVERITY_HIGH, 0)
	spec.GetScanGate().SetLicenses(licenses)

	assert.Equal(t, []FieldValue{
		{Path: "scan_gate.fail_on", Value: "high", Human: "high"},
		{Path: "scan_gate.licenses.deny", Value: "GPL-3.0-only, AGPL-3.0-only", Human: "GPL-3.0-only, AGPL-3.0-only"},
		{Path: "scan_gate.licenses.allow", Value: "[]", Human: "[]"},
	}, Fields(spec))
}

func TestLeafPathsAndCapabilities(t *testing.T) {
	paths := LeafPaths()
	assert.Contains(t, paths, "scan_gate.fail_on")
	assert.Contains(t, paths, "scan_gate.fail_on_attacks")
	assert.Contains(t, paths, "package_admission.cooldown.min_age")
	assert.Contains(t, paths, "scan_gate.licenses.deny", "a list setting is one setting")
	assert.Contains(t, paths, "scan_gate.licenses.unknown")
	assert.NotContains(t, paths, "scan_gate.licenses.deny.licenses")
	for _, p := range paths {
		assert.NotContains(t, p, "core", "package rules are not settings")
	}
	assert.Equal(t, []string{"package_admission", "scan_gate"}, Capabilities())
}

func TestValidateFieldPaths(t *testing.T) {
	require.NoError(t, ValidateFieldPaths(nil))
	require.NoError(t, ValidateFieldPaths([]string{"scan_gate.fail_on", "package_admission"}))
	err := ValidateFieldPaths([]string{"scan_gate.fail"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown policy field "scan_gate.fail"`)
}

func TestMatchesFieldFilter(t *testing.T) {
	assert.True(t, MatchesFieldFilter([]string{"scan_gate.fail_on"}, nil))
	assert.True(t, MatchesFieldFilter([]string{"scan_gate.fail_on"}, []string{"scan_gate"}))
	assert.False(t, MatchesFieldFilter([]string{"scan_gate.fail_on_attacks"}, []string{"scan_gate.fail_on"}))
}

func TestFormatJSONValue(t *testing.T) {
	tests := []struct {
		path, raw, value, human string
	}{
		{"scan_gate.fail_on", `"SEVERITY_MEDIUM"`, "medium", "medium"},
		// The server writes an enum by its value name, with no quotes.
		{"scan_gate.fail_on", "SEVERITY_MEDIUM", "medium", "medium"},
		{"scan_gate.fail_on_attacks", "ATTACK_GATE_FAIL", "fail", "fail"},
		{"package_admission.cooldown.min_age", `"604800s"`, "168h", "7d"},
		{"package_admission.cooldown.min_age", `"0s"`, "0s", "0s"},
		{"scan_gate.fail_on", "", "-", "-"},
		{"scan_gate.licenses.deny", `{"licenses":["GPL-3.0-only","MIT"]}`, "GPL-3.0-only, MIT", "GPL-3.0-only, MIT"},
		{"scan_gate.licenses.deny", `{}`, "[]", "[]"},
		{"scan_gate.licenses.allow", `["MIT"]`, "MIT", "MIT"},
		// A server that walks into the wrapper reports the inner list.
		{"scan_gate.licenses.deny.licenses", `["GPL-3.0-only","MIT"]`, "GPL-3.0-only, MIT", "GPL-3.0-only, MIT"},
		{"scan_gate.licenses.deny.licenses", `[]`, "[]", "[]"},
		{"unknown.path", `"text"`, "text", "text"},
		{"unknown.path", `{"a":1}`, `{"a":1}`, `{"a":1}`},
		{"unknown.path", `not json`, "not json", "not json"},
	}
	for _, tt := range tests {
		value, human := FormatJSONValue(tt.path, tt.raw)
		assert.Equal(t, tt.value, value, tt.raw)
		assert.Equal(t, tt.human, human, tt.raw)
	}
}

// TestLeafPathsMergeByReplace guards the source of a value in `safedep policy
// show`, which takes the value of the nearest layer that sets the path. A
// path that merges by union or by key gets items from more than one layer,
// so a new such path must change that view first.
func TestLeafPathsMergeByReplace(t *testing.T) {
	paths := LeafPaths()
	require.NotEmpty(t, paths)
	for _, path := range paths {
		fd := fieldByPath(path)
		require.NotNil(t, fd, path)
		contract, ok := proto.GetExtension(fd.Options(), optionsv1.E_Field).(*optionsv1.FieldContract)
		require.True(t, ok, path)
		assert.Equal(t, optionsv1.Merge_MERGE_REPLACE, contract.GetMerge(), path)
	}
}

func TestWrapWords(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{name: "fits", text: "one two", width: 7, want: []string{"one two"}},
		{name: "breaks at spaces", text: "one two three", width: 7, want: []string{"one two", "three"}},
		{name: "long word keeps its own line", text: "a verylongword b", width: 5, want: []string{"a", "verylongword", "b"}},
		{name: "counts display columns, not bytes", text: "ab · cd · ef", width: 7, want: []string{"ab · cd", "· ef"}},
		{name: "empty", text: "", width: 5, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, WrapWords(tt.text, tt.width))
		})
	}
}
