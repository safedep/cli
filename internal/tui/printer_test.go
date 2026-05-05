package tui

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
		{"table", ModeTable, false},
		{"plain", ModePlain, false},
		{"json", ModeJSON, false},
		{"rich", "", true},
		{"agent", "", true},
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
	assert.Contains(t, []Mode{ModeTable, ModePlain, ModeJSON}, got)
}
