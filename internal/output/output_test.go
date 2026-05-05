package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMode(t *testing.T) {
	cases := []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{"rich", ModeRich, false},
		{"plain", ModePlain, false},
		{"agent", ModeAgent, false},
		{"json", ModeJSON, false},
		{"table", "", true},
		{"yaml", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseMode(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseMode_emptyAutoDetects(t *testing.T) {
	got, err := ParseMode("")
	require.NoError(t, err)
	assert.Contains(t, []Mode{ModeRich, ModePlain, ModeAgent}, got, "auto-detect must never return JSON")
}
