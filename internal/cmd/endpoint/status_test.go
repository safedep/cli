package endpoint

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStats struct {
	in  StatsInput
	out *StatsResult
	err error
}

func (f *fakeStats) Stats(_ context.Context, in StatsInput) (*StatsResult, error) {
	f.in = in
	return f.out, f.err
}

func TestRunStatus_propagatesWindow(t *testing.T) {
	f := &fakeStats{out: &StatsResult{TotalEndpoints: 10, ActiveEndpoints: 7, SilentEndpoints: 3, PMGBlockedEvents: 2}}
	in := statusInput{Window: TimeWindow{Start: time.Unix(1000, 0), End: time.Unix(2000, 0)}}
	res, err := runStatus(context.Background(), f, in)
	require.NoError(t, err)
	assert.Equal(t, uint64(10), res.data.TotalEndpoints)
	assert.Equal(t, time.Unix(1000, 0), f.in.Window.Start)
}
