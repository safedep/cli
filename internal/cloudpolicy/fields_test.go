package cloudpolicy

import (
	"testing"
	"time"

	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFields(t *testing.T) {
	got := Fields(specWith(scangatev1.Severity_SEVERITY_MEDIUM, 168*time.Hour))
	assert.Equal(t, []FieldValue{
		{Path: "package_admission.cooldown.min_age", Value: "168h", Human: "7d"},
		{Path: "scan_gate.fail_on", Value: "medium", Human: "medium"},
	}, got)
	assert.Empty(t, Fields(nil))
}

func TestLeafPathsAndCapabilities(t *testing.T) {
	paths := LeafPaths()
	assert.Contains(t, paths, "scan_gate.fail_on")
	assert.Contains(t, paths, "scan_gate.fail_on_attacks")
	assert.Contains(t, paths, "package_admission.cooldown.min_age")
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
