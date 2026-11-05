package duration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "30d", want: 30 * 24 * time.Hour},
		{in: "1d12h", want: 36 * time.Hour},
		{in: "7d", want: 168 * time.Hour},
		{in: "48h", want: 48 * time.Hour},
		{in: "90m", want: 90 * time.Minute},
		{in: " 2d ", want: 48 * time.Hour},
		{in: "0s", want: 0},
		{in: "d", wantErr: true},
		{in: "30days", wantErr: true},
		{in: "1d-1h", wantErr: true},
		{in: "200000d", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{48 * time.Hour, "48h"},
		{168 * time.Hour, "168h"},
		{90 * time.Minute, "1h30m"},
		{30 * time.Second, "30s"},
		{time.Hour + 30*time.Second, "1h0m30s"},
		{1500 * time.Millisecond, "1.5s"},
	}
	for _, tt := range tests {
		got := Format(tt.in)
		assert.Equal(t, tt.want, got)
		back, err := Parse(got)
		require.NoError(t, err)
		assert.Equal(t, tt.in, back, "Format must round trip through Parse")
	}
}

func TestValue(t *testing.T) {
	var d time.Duration
	v := NewValue(&d, 7*24*time.Hour)
	assert.Equal(t, "168h", v.String())
	require.NoError(t, v.Set("30d"))
	assert.Equal(t, 30*24*time.Hour, d)
	require.Error(t, v.Set("soon"))
	assert.Equal(t, "duration", v.Type())
}
