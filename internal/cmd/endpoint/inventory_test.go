package endpoint

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInvSvc struct {
	out    *InventoryEventsResult
	pages  map[string]*InventoryEventsResult
	inputs []InventoryEventsInput
}

func (f *fakeInvSvc) ListInventoryEvents(_ context.Context, in InventoryEventsInput) (*InventoryEventsResult, error) {
	f.inputs = append(f.inputs, in)
	if f.pages != nil {
		if out, ok := f.pages[in.PageToken]; ok {
			return out, nil
		}
		return &InventoryEventsResult{}, nil
	}
	return f.out, nil
}

func TestRunInventory_dedupesByItemIdentityKeepingLatest(t *testing.T) {
	older := time.Unix(100, 0)
	newer := time.Unix(200, 0)
	f := &fakeInvSvc{out: &InventoryEventsResult{Events: []InventoryEvent{
		{Timestamp: older, ItemIdentity: "lodash-id", Name: "lodash", App: "npm"},
		{Timestamp: newer, ItemIdentity: "lodash-id", Name: "lodash", App: "npm-newer"},
		{Timestamp: older, ItemIdentity: "claude-code-id", Name: "claude-code"},
	}}}
	res, err := runInventory(context.Background(), f, nil, inventoryInput{})
	require.NoError(t, err)
	require.Len(t, res.items, 2)
	var lodash *InventoryEvent
	for i := range res.items {
		if res.items[i].ItemIdentity == "lodash-id" {
			lodash = &res.items[i]
		}
	}
	require.NotNil(t, lodash)
	assert.Equal(t, "npm-newer", lodash.App, "later event should win")
}

func TestRunInventory_filtersNoiseEvents(t *testing.T) {
	f := &fakeInvSvc{out: &InventoryEventsResult{Events: []InventoryEvent{
		{Timestamp: time.Unix(100, 0)}, // unspecified kind, no name/app/identity
		{Timestamp: time.Unix(200, 0), ItemIdentity: "real", Name: "real-thing"},
	}}}
	res, err := runInventory(context.Background(), f, nil, inventoryInput{})
	require.NoError(t, err)
	require.Len(t, res.items, 1)
	assert.Equal(t, "real", res.items[0].ItemIdentity)
}

func TestRunInventory_allPagesFetchesAndDedupesAcrossPages(t *testing.T) {
	f := &fakeInvSvc{pages: map[string]*InventoryEventsResult{
		"": {
			Events: []InventoryEvent{
				{Timestamp: time.Unix(100, 0), ItemIdentity: "shared", Name: "shared", App: "older"},
			},
			NextPage: "tok-1",
		},
		"tok-1": {
			Events: []InventoryEvent{
				{Timestamp: time.Unix(200, 0), ItemIdentity: "shared", Name: "shared", App: "newer"},
				{Timestamp: time.Unix(150, 0), ItemIdentity: "other", Name: "other"},
			},
		},
	}}

	res, err := runInventory(context.Background(), f, nil, inventoryInput{AllPages: true, PageSize: 1})

	require.NoError(t, err)
	require.Len(t, f.inputs, 2)
	assert.Equal(t, "", f.inputs[0].PageToken)
	assert.Equal(t, "tok-1", f.inputs[1].PageToken)
	assert.Empty(t, res.nextPage)

	require.Len(t, res.items, 2)
	var shared *InventoryEvent
	for i := range res.items {
		if res.items[i].ItemIdentity == "shared" {
			shared = &res.items[i]
		}
	}
	require.NotNil(t, shared)
	assert.Equal(t, "newer", shared.App)
}

func TestRunInventory_allPagesDetectsPaginationLoop(t *testing.T) {
	f := &fakeInvSvc{pages: map[string]*InventoryEventsResult{
		"": {
			Events:    []InventoryEvent{{Timestamp: time.Unix(100, 0), ItemIdentity: "a", Name: "a"}},
			NextPage: "tok-loop",
		},
		"tok-loop": {
			Events:    []InventoryEvent{{Timestamp: time.Unix(200, 0), ItemIdentity: "b", Name: "b"}},
			NextPage: "tok-loop",
		},
	}}

	_, err := runInventory(context.Background(), f, nil, inventoryInput{AllPages: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pagination loop detected")
}
