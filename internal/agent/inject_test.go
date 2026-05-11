package agent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes ---

type fakeGlobalInjector struct {
	path      string
	injected  *MCPConfig
	removed   bool
	injectErr error
	removeErr error
}

func (f *fakeGlobalInjector) GlobalConfigPath() string { return f.path }
func (f *fakeGlobalInjector) InjectGlobal(cfg MCPConfig) error {
	if f.injectErr != nil {
		return f.injectErr
	}
	c := cfg
	f.injected = &c
	return nil
}
func (f *fakeGlobalInjector) RemoveGlobal() error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = true
	return nil
}

type fakeWorkspaceInjector struct {
	path      string
	injected  *MCPConfig
	removed   bool
	injectErr error
	removeErr error
}

func (f *fakeWorkspaceInjector) WorkspaceConfigPath(_ string) string { return f.path }
func (f *fakeWorkspaceInjector) InjectWorkspace(_ string, cfg MCPConfig) error {
	if f.injectErr != nil {
		return f.injectErr
	}
	c := cfg
	f.injected = &c
	return nil
}
func (f *fakeWorkspaceInjector) RemoveWorkspace(_ string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = true
	return nil
}

type fakeAgent struct {
	name      string
	detected  bool
	global    *fakeGlobalInjector
	workspace *fakeWorkspaceInjector
}

func (f *fakeAgent) Name() string    { return f.name }
func (f *fakeAgent) Detected() bool  { return f.detected }
func (f *fakeAgent) AsGlobalInjector() (GlobalInjector, bool) {
	if f.global == nil {
		return nil, false
	}
	return f.global, true
}
func (f *fakeAgent) AsWorkspaceInjector() (WorkspaceInjector, bool) {
	if f.workspace == nil {
		return nil, false
	}
	return f.workspace, true
}

// --- tests ---

func TestInjectAll(t *testing.T) {
	cfg := MCPConfig{URL: "https://mcp.safedep.io"}

	t.Run("skips undetected agents", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "x", detected: false, global: gi}

		require.NoError(t, InjectAll([]Agent{a}, cfg, ""))
		assert.Nil(t, gi.injected)
	})

	t.Run("injects global for detected agent", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "x", detected: true, global: gi}

		require.NoError(t, InjectAll([]Agent{a}, cfg, ""))
		require.NotNil(t, gi.injected)
		assert.Equal(t, cfg.URL, gi.injected.URL)
	})

	t.Run("skips workspace when workspaceDir is empty", func(t *testing.T) {
		wi := &fakeWorkspaceInjector{}
		a := &fakeAgent{name: "x", detected: true, workspace: wi}

		require.NoError(t, InjectAll([]Agent{a}, cfg, ""))
		assert.Nil(t, wi.injected)
	})

	t.Run("injects workspace when workspaceDir is set", func(t *testing.T) {
		wi := &fakeWorkspaceInjector{}
		a := &fakeAgent{name: "x", detected: true, workspace: wi}

		require.NoError(t, InjectAll([]Agent{a}, cfg, "/project"))
		require.NotNil(t, wi.injected)
	})

	t.Run("accumulates errors and continues across agents", func(t *testing.T) {
		gi1 := &fakeGlobalInjector{injectErr: errors.New("agent-1-fail")}
		gi2 := &fakeGlobalInjector{}
		a1 := &fakeAgent{name: "a1", detected: true, global: gi1}
		a2 := &fakeAgent{name: "a2", detected: true, global: gi2}

		err := InjectAll([]Agent{a1, a2}, cfg, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agent-1-fail")
		assert.NotNil(t, gi2.injected, "a2 must still be attempted after a1 fails")
	})

	t.Run("agent with no global injector is silently skipped", func(t *testing.T) {
		a := &fakeAgent{name: "x", detected: true, global: nil}
		require.NoError(t, InjectAll([]Agent{a}, cfg, ""))
	})
}

func TestRemoveAll(t *testing.T) {
	t.Run("removes from detected agent", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "x", detected: true, global: gi}

		require.NoError(t, RemoveAll([]Agent{a}, ""))
		assert.True(t, gi.removed)
	})

	t.Run("skips undetected agent", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "x", detected: false, global: gi}

		require.NoError(t, RemoveAll([]Agent{a}, ""))
		assert.False(t, gi.removed)
	})

	t.Run("accumulates errors and continues", func(t *testing.T) {
		gi1 := &fakeGlobalInjector{removeErr: errors.New("remove-fail")}
		gi2 := &fakeGlobalInjector{}
		a1 := &fakeAgent{name: "a1", detected: true, global: gi1}
		a2 := &fakeAgent{name: "a2", detected: true, global: gi2}

		err := RemoveAll([]Agent{a1, a2}, "")
		require.Error(t, err)
		assert.True(t, gi2.removed, "a2 must still be attempted")
	})

	t.Run("removes workspace when workspaceDir is set", func(t *testing.T) {
		wi := &fakeWorkspaceInjector{}
		a := &fakeAgent{name: "x", detected: true, workspace: wi}

		require.NoError(t, RemoveAll([]Agent{a}, "/project"))
		assert.True(t, wi.removed)
	})
}
