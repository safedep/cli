package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeminiCLIStub(t *testing.T) {
	g := newGeminiCLI(t.TempDir())
	assert.Equal(t, "gemini-cli", g.Name())
	assert.False(t, g.Detected())
	_, ok := g.AsGlobalInjector()
	assert.False(t, ok)
	_, ok = g.AsWorkspaceInjector()
	assert.False(t, ok)
}
