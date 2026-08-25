package jfrog

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCursorStore_RemoveClearsSavedCursor(t *testing.T) {
	ctx := context.Background()
	store := newCursorStore(newTestKV(t))

	require.NoError(t, store.save(ctx, cursorState{LastSeenAt: time.Now().UTC()}))

	require.NoError(t, store.remove(ctx))

	got, err := store.load(ctx)
	require.NoError(t, err)
	assert.True(t, got.LastSeenAt.IsZero(), "remove clears the stored cursor")

	// Remove on an already-empty store is a safe no-op.
	assert.NoError(t, store.remove(ctx))
}
