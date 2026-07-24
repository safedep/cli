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

func TestIsInteractive_AgentModeIsNeverInteractive(t *testing.T) {
	prev := output.CurrentMode()
	defer output.SetMode(prev)

	output.SetMode(output.Agent)
	assert.False(t, IsInteractive(), "agent mode must never be interactive")
}
