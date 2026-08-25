package jfrog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDisplayVersions(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{name: "empty means all", versions: nil, want: "all"},
		{name: "only empty entries means all", versions: []string{"", ""}, want: "all"},
		{name: "single version", versions: []string{"1.0.0"}, want: "1.0.0"},
		{name: "many versions joined", versions: []string{"9.9.9", "9.9.10"}, want: "9.9.9, 9.9.10"},
		{name: "empty entries dropped", versions: []string{"", "1.0.0", ""}, want: "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, displayVersions(tt.versions))
		})
	}
}
