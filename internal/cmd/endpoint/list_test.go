package endpoint

import (
	"context"
	"testing"
	"time"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLister struct {
	in  ListInput
	out *ListResult
}

func (f *fakeLister) List(_ context.Context, in ListInput) (*ListResult, error) {
	f.in = in
	return f.out, nil
}

func TestRunList_passesFilters(t *testing.T) {
	f := &fakeLister{out: &ListResult{Endpoints: []ListEndpoint{{ID: "01KR0EKN6PMW0ZRFRN992H1PKX", Hostname: "x"}}}}
	in := listInput{
		Window:       TimeWindow{Start: time.Unix(0, 0), End: time.Unix(100, 0)},
		Capabilities: []controltowerv1.EndpointCapability{controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_PACKAGE_GUARD},
		OnlyBlocked:  true,
		SilentFor:    7 * 24 * time.Hour,
		Search:       "x",
		PageSize:     50,
	}
	res, err := runList(context.Background(), f, newNopDirectory(t), in)
	require.NoError(t, err)
	assert.Len(t, res.endpoints, 1)
	assert.Equal(t, []controltowerv1.EndpointCapability{controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_PACKAGE_GUARD}, f.in.Capabilities)
	assert.Equal(t, uint64(1), f.in.MinPMGBlocked)
	assert.Equal(t, uint32(50), f.in.PageSize)
}

func TestMapCapabilities_validatesAndNormalizes(t *testing.T) {
	caps, err := mapCapabilities([]string{" GUARD ", "inventory"})
	require.NoError(t, err)
	assert.Equal(t, []controltowerv1.EndpointCapability{
		controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_PACKAGE_GUARD,
		controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_INVENTORY,
	}, caps)

	_, err = mapCapabilities([]string{"unknown"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use guard|tracer|advisor|inventory")
}

func TestRunList_clientSideSilentFilter(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	f := &fakeLister{out: &ListResult{Endpoints: []ListEndpoint{
		{ID: "01KR0EKN6PMW0ZRFRN992H1PKX", LastSync: now.Add(-1 * time.Hour)},          // active
		{ID: "01KR0EKN6PMW0ZRFRN992H1PKY", LastSync: now.Add(-10 * 24 * time.Hour)}, // silent
	}}}
	in := listInput{SilentFor: 7 * 24 * time.Hour, _now: func() time.Time { return now }}
	res, err := runList(context.Background(), f, newNopDirectory(t), in)
	require.NoError(t, err)
	assert.Len(t, res.endpoints, 1)
	assert.Equal(t, "01KR0EKN6PMW0ZRFRN992H1PKY", res.endpoints[0].ID)
}

func newNopDirectory(t *testing.T) *Directory {
	t.Helper()
	return NewDirectory(newFakeStore(), time.Now)
}
