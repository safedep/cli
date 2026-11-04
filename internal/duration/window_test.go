package duration

import (
	"testing"
	"time"

	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindow(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	tests := []struct {
		name       string
		since      time.Duration
		until      time.Duration
		wantAfter  *time.Time
		wantBefore *time.Time
		wantErr    string
	}{
		{name: "no bounds"},
		{name: "since only", since: 7 * day, wantAfter: new(now.Add(-7 * day))},
		{name: "until only", until: day, wantBefore: new(now.Add(-day))},
		{name: "both", since: 7 * day, until: day, wantAfter: new(now.Add(-7 * day)), wantBefore: new(now.Add(-day))},
		{name: "until not shorter", since: time.Hour, until: time.Hour, wantErr: "--until must be shorter than --since"},
		{name: "negative", since: -time.Hour, wantErr: "must not be negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			after, before, err := Window(tt.since, tt.until, now)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				ue, ok := usefulerror.AsUsefulError(err)
				require.True(t, ok)
				assert.Equal(t, usefulerror.ErrBadRequest, ue.Code())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAfter, after)
			assert.Equal(t, tt.wantBefore, before)
		})
	}
}
