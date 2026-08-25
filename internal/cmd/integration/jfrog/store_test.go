package jfrog

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCursorStore_ResetClearsSavedCursor(t *testing.T) {
	ctx := context.Background()
	store := newCursorStore(newTestKV(t))

	require.NoError(t, store.save(ctx, cursorState{LastSeenAt: time.Now().UTC()}))

	require.NoError(t, store.reset(ctx))

	got, err := store.load(ctx)
	require.NoError(t, err)
	assert.True(t, got.LastSeenAt.IsZero(), "reset clears the stored cursor")

	// Reset on an already-empty store is a safe no-op.
	assert.NoError(t, store.reset(ctx))
}
