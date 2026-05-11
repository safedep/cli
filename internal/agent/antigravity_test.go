package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAntigravityStub(t *testing.T) {
	a := newAntigravity(t.TempDir())
	assert.Equal(t, "antigravity", a.Name())
	assert.False(t, a.Detected())
	_, ok := a.AsGlobalInjector()
	assert.False(t, ok)
	_, ok = a.AsWorkspaceInjector()
	assert.False(t, ok)
}
