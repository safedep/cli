package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry(t *testing.T) {
	agents := NewRegistry()
	require.NotEmpty(t, agents)

	names := make(map[string]bool, len(agents))
	for _, a := range agents {
		names[a.Name()] = true
	}

	assert.True(t, names["claude-code"], "registry must include claude-code")
	assert.True(t, names["cursor"], "registry must include cursor")
	assert.True(t, names["vscode"], "registry must include vscode")
	assert.True(t, names["gemini-cli"], "registry must include gemini-cli")
	assert.True(t, names["opencode"], "registry must include opencode")
	assert.True(t, names["antigravity"], "registry must include antigravity")
}
