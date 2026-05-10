package endpoint

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeFullSvc struct {
	getOut     *GetResult
	getErr     error
	guardOut   *GuardEventsResult
	guardErr   error
	invOut     *InventoryEventsResult
	invErr     error
	capturedID string
}

func (f *fakeFullSvc) Get(_ context.Context, in GetInput) (*GetResult, error) {
	f.capturedID = in.EndpointID
	return f.getOut, f.getErr
}

func (f *fakeFullSvc) ListGuardEvents(_ context.Context, _ GuardEventsInput) (*GuardEventsResult, error) {
	return f.guardOut, f.guardErr
}

func (f *fakeFullSvc) ListInventoryEvents(_ context.Context, _ InventoryEventsInput) (*InventoryEventsResult, error) {
	return f.invOut, f.invErr
}

func TestRunShow_resolvesByHostname(t *testing.T) {
	dir := newNopDirectory(t)
	require.NoError(t, dir.Upsert(context.Background(), []DirectoryEntry{{ID: "01KR0EKN6PMW0ZRFRN992H1PKX", Hostname: "lap"}}))
	svc := &fakeFullSvc{
		getOut:   &GetResult{ID: "01KR0EKN6PMW0ZRFRN992H1PKX", Hostname: "lap"},
		guardOut: &GuardEventsResult{},
		invOut:   &InventoryEventsResult{},
	}
	res, err := runShow(context.Background(), svc, dir, showInput{Ref: "lap", Window: TimeWindow{Start: time.Unix(0, 0), End: time.Unix(1, 0)}})
	require.NoError(t, err)
	assert.Equal(t, "01KR0EKN6PMW0ZRFRN992H1PKX", svc.capturedID)
	assert.Equal(t, "lap", res.endpoint.Hostname)
}

func TestRunShow_propagatesGetError(t *testing.T) {
	svc := &fakeFullSvc{getErr: errors.New("boom")}
	_, err := runShow(context.Background(), svc, newNopDirectory(t), showInput{Ref: "01KR0EKN6PMW0ZRFRN992H1PKX"})
	require.Error(t, err)
}

func TestRunShow_secondaryFailuresDoNotBlock(t *testing.T) {
	svc := &fakeFullSvc{
		getOut:   &GetResult{ID: "01KR0EKN6PMW0ZRFRN992H1PKX"},
		guardErr: errors.New("guard down"),
		invErr:   errors.New("inv down"),
	}
	res, err := runShow(context.Background(), svc, newNopDirectory(t), showInput{Ref: "01KR0EKN6PMW0ZRFRN992H1PKX"})
	require.NoError(t, err)
	assert.Empty(t, res.recentBlocks)
	assert.Equal(t, 0, res.inventoryCount)
}
