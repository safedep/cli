package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenCodeStub(t *testing.T) {
	o := newOpenCode(t.TempDir())
	assert.Equal(t, "opencode", o.Name())
	assert.False(t, o.Detected())
	_, ok := o.AsGlobalInjector()
	assert.False(t, ok)
	_, ok = o.AsWorkspaceInjector()
	assert.False(t, ok)
}
