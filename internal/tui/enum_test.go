package tui

import (
	"testing"

	"github.com/safedep/dry/tui/output"
	"github.com/stretchr/testify/assert"
)

func TestEnumToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, prefix, want string
	}{
		{"ANALYSIS_STATUS_IN_PROGRESS", "ANALYSIS_STATUS_", "in-progress"},
		{"BILLING_TIER_PROFESSIONAL", "BILLING_TIER_", "professional"},
		{"ECOSYSTEM_NPM", "ECOSYSTEM_", "npm"},
		{"ECOSYSTEM_UNSPECIFIED", "ECOSYSTEM_", "unknown"},
		{"", "ECOSYSTEM_", "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, EnumToken(tt.in, tt.prefix), tt.in)
	}
}

// scanStatusNames mirrors a generated proto `<Enum>_name` map, including the
// non-contiguous numbering a proto can carry.
var scanStatusNames = map[int32]string{
	0: "SCAN_STATUS_UNSPECIFIED",
	1: "SCAN_STATUS_SUCCESS",
	2: "SCAN_STATUS_ERROR",
	3: "SCAN_STATUS_QUEUED",
	7: "SCAN_STATUS_IN_PROGRESS",
}

func TestParseEnumToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in        string
		want      int32
		wantFound bool
	}{
		{in: "success", want: 1, wantFound: true},
		{in: "queued", want: 3, wantFound: true},
		{in: "in-progress", want: 7, wantFound: true},
		{in: "unknown"},
		{in: "SUCCESS"},
		{in: "scan_status_success"},
		{in: ""},
	}
	for _, tt := range tests {
		got, found := ParseEnumToken(scanStatusNames, "SCAN_STATUS_", tt.in)
		assert.Equal(t, tt.wantFound, found, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
	}
}

func TestParseEnumToken_RoundTripsEveryListedToken(t *testing.T) {
	t.Parallel()
	for _, token := range EnumTokens(scanStatusNames, "SCAN_STATUS_") {
		number, found := ParseEnumToken(scanStatusNames, "SCAN_STATUS_", token)
		assert.True(t, found, token)
		assert.Equal(t, token, EnumToken(scanStatusNames[number], "SCAN_STATUS_"))
	}
}

func TestEnumTokens_IsOrderedByEnumNumberAndSkipsUnspecified(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		[]string{"success", "error", "queued", "in-progress"},
		EnumTokens(scanStatusNames, "SCAN_STATUS_"),
	)
	assert.Empty(t, EnumTokens(map[int32]string{0: "SCAN_STATUS_UNSPECIFIED"}, "SCAN_STATUS_"))
	assert.Empty(t, EnumTokens(nil, "SCAN_STATUS_"))
}

func TestIsInteractive_AgentModeIsNeverInteractive(t *testing.T) {
	prev := output.CurrentMode()
	defer output.SetMode(prev)

	output.SetMode(output.Agent)
	assert.False(t, IsInteractive(), "agent mode must never be interactive")
}
