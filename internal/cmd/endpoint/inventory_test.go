package endpoint

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInvSvc struct {
	out *InventoryEventsResult
	in  InventoryEventsInput
}

func (f *fakeInvSvc) ListInventoryEvents(_ context.Context, in InventoryEventsInput) (*InventoryEventsResult, error) {
	f.in = in
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
