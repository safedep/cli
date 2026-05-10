package endpoint

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeActivitySvc struct {
	guard   *GuardEventsResult
	inv     *InventoryEventsResult
	guardIn GuardEventsInput
	invIn   InventoryEventsInput
}

func (f *fakeActivitySvc) ListGuardEvents(_ context.Context, in GuardEventsInput) (*GuardEventsResult, error) {
	f.guardIn = in
	return f.guard, nil
}

func (f *fakeActivitySvc) ListInventoryEvents(_ context.Context, in InventoryEventsInput) (*InventoryEventsResult, error) {
	f.invIn = in
	return f.inv, nil
}

func TestRunActivity_typeAllMergesByTime(t *testing.T) {
	t1 := time.Unix(100, 0)
	t2 := time.Unix(200, 0)
	t3 := time.Unix(150, 0)
	svc := &fakeActivitySvc{
		guard: &GuardEventsResult{Events: []GuardEvent{
			{Timestamp: t1, Action: "blocked", PackageName: "lodash"},
			{Timestamp: t2, Action: "blocked", PackageName: "left-pad"},
		}},
		inv: &InventoryEventsResult{Events: []InventoryEvent{
			{Timestamp: t3, ItemIdentity: "claude-code", Name: "claude-code"},
		}},
	}
	res, err := runActivity(context.Background(), svc, nil, activityInput{Type: "all", PageSize: 10})
	require.NoError(t, err)
	require.Len(t, res.rows, 3)
	assert.Equal(t, t2, res.rows[0].Timestamp)
	assert.Equal(t, t3, res.rows[1].Timestamp)
	assert.Equal(t, t1, res.rows[2].Timestamp)
}

func TestRunActivity_defaultActionsIncludeCooldown(t *testing.T) {
	svc := &fakeActivitySvc{guard: &GuardEventsResult{}, inv: &InventoryEventsResult{}}
	_, err := runActivity(context.Background(), svc, nil, activityInput{Type: "guard", PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, []GuardAction{"blocked", "cooldown-blocked"}, svc.guardIn.Actions)
}

func TestRunActivity_typeGuardSkipsInventory(t *testing.T) {
	svc := &fakeActivitySvc{guard: &GuardEventsResult{}, inv: nil}
	_, err := runActivity(context.Background(), svc, nil, activityInput{Type: "guard"})
	require.NoError(t, err)
	// inv handler not called -> invIn zero value
	assert.Empty(t, svc.invIn.EndpointIDs)
}

func TestRunActivity_emptyTypeDefaultsToGuard(t *testing.T) {
	svc := &fakeActivitySvc{guard: &GuardEventsResult{}, inv: nil}

	_, err := runActivity(context.Background(), svc, nil, activityInput{})

	require.NoError(t, err)
	assert.Equal(t, []GuardAction{"blocked", "cooldown-blocked"}, svc.guardIn.Actions)
	assert.Empty(t, svc.invIn.EndpointIDs)
}

func TestRunActivity_unknownTypeReturnsError(t *testing.T) {
	svc := &fakeActivitySvc{guard: &GuardEventsResult{}, inv: &InventoryEventsResult{}}

	_, err := runActivity(context.Background(), svc, nil, activityInput{Type: "invalid-type"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown activity type")
	assert.Contains(t, err.Error(), "all|guard|inventory")
}
