package cloudpolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDocumentPath(t *testing.T) {
	tests := []struct {
		path     string
		want     string
		duration bool
	}{
		{"base.package_admission.cooldown.min_age", "base.package_admission.cooldown.min_age", true},
		{`overrides[0].selector.match_labels["team/x"]`, `overrides[0].selector.matchLabels["team/x"]`, false},
		{`overrides[0].selector.match_labels["a.b[c]"]`, `overrides[0].selector.matchLabels["a.b[c]"]`, false},
		{"overrides[1].selector.match_expressions[0].key", "overrides[1].selector.matchExpressions[0].key", false},
		{"overrides[2].override_id", "overrides[2].id", false},
		{"overrides[2].expires_at", "overrides[2].expires_at", false},
		{"overrides[0].spec.scan_gate.fail_on", "overrides[0].spec.scan_gate.fail_on", false},
		{"base_description", "base_description", false},
		{"unknown.match_labels", "unknown.match_labels", false},
		{"overrides[0.name", "overrides[0.name", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, duration := DocumentPath(tt.path)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.duration, duration)
		})
	}
}

func TestDocumentDurations(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{
			"must be greater than or equal to 0s and less than or equal to 31536000s",
			"must be greater than or equal to 0s and less than or equal to 365d",
		},
		{"must be at most 604800s", "must be at most 7d"},
		{"must be at most 3600s", "must be at most 3600s"},
		{"must be at most 1.5s", "must be at most 1.5s"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, DocumentDurations(tt.in))
		})
	}
}
