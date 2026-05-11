package mcp

import (
	"errors"
	"testing"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/safedep/cli/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes ---

type fakeResolver struct {
	identity *controltowerv1.EndpointIdentity
	err      error
}

func (f *fakeResolver) Resolve() (*controltowerv1.EndpointIdentity, error) {
	return f.identity, f.err
}

type fakeGlobalInjector struct {
	injected  *agent.MCPConfig
	removed   bool
	injectErr error
}

func (f *fakeGlobalInjector) GlobalConfigPath() string { return "/fake/path" }
func (f *fakeGlobalInjector) InjectGlobal(cfg agent.MCPConfig) error {
	if f.injectErr != nil {
		return f.injectErr
	}
	c := cfg
	f.injected = &c
	return nil
}
func (f *fakeGlobalInjector) RemoveGlobal() error {
	f.removed = true
	return nil
}

type fakeAgent struct {
	name     string
	detected bool
	global   *fakeGlobalInjector
}

func (f *fakeAgent) Name() string   { return f.name }
func (f *fakeAgent) Detected() bool { return f.detected }
func (f *fakeAgent) AsGlobalInjector() (agent.GlobalInjector, bool) {
	if f.global == nil {
		return nil, false
	}
	return f.global, true
}
func (f *fakeAgent) AsWorkspaceInjector() (agent.WorkspaceInjector, bool) {
	return nil, false
}

// --- tests ---

var testIdentity = &controltowerv1.EndpointIdentity{
	Identifier: "test-host",
	MachineId:  "test-machine-id",
}

func TestMCPServiceInstall(t *testing.T) {
	t.Run("injects into detected agent with correct headers", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "claude-code", detected: true, global: gi}
		svc := newMCPService([]agent.Agent{a}, &fakeResolver{identity: testIdentity})

		require.NoError(t, svc.install(installInput{
			MCPURL:   "https://mcp.safedep.io/v1",
			APIKey:   "key123",
			TenantID: "tenant-1",
		}))
		require.NotNil(t, gi.injected)
		assert.Equal(t, "https://mcp.safedep.io/v1", gi.injected.URL)
		assert.Equal(t, "Bearer key123", gi.injected.Headers["Authorization"])
		assert.Equal(t, "tenant-1", gi.injected.Headers["X-Tenant-ID"])
		assert.NotEmpty(t, gi.injected.Headers["X-Endpoint-ID"])
	})

	t.Run("skips undetected agents", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "cursor", detected: false, global: gi}
		svc := newMCPService([]agent.Agent{a}, &fakeResolver{identity: testIdentity})

		require.NoError(t, svc.install(installInput{
			MCPURL:   "https://mcp.safedep.io/v1",
			APIKey:   "k",
			TenantID: "t",
		}))
		assert.Nil(t, gi.injected)
	})

	t.Run("returns error when identity resolver fails", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "claude-code", detected: true, global: gi}
		svc := newMCPService([]agent.Agent{a}, &fakeResolver{err: errors.New("no machine id")})

		err := svc.install(installInput{
			MCPURL:   "https://mcp.safedep.io/v1",
			APIKey:   "k",
			TenantID: "t",
		})
		require.Error(t, err)
		assert.Nil(t, gi.injected)
	})

	t.Run("no detected agents returns nil", func(t *testing.T) {
		svc := newMCPService([]agent.Agent{}, &fakeResolver{identity: testIdentity})
		require.NoError(t, svc.install(installInput{
			MCPURL:   "https://mcp.safedep.io/v1",
			APIKey:   "k",
			TenantID: "t",
		}))
	})
}

func TestMCPServiceUninstall(t *testing.T) {
	t.Run("removes from detected agent", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "claude-code", detected: true, global: gi}
		svc := newMCPService([]agent.Agent{a}, nil)

		require.NoError(t, svc.uninstall(uninstallInput{}))
		assert.True(t, gi.removed)
	})

	t.Run("skips undetected agents", func(t *testing.T) {
		gi := &fakeGlobalInjector{}
		a := &fakeAgent{name: "cursor", detected: false, global: gi}
		svc := newMCPService([]agent.Agent{a}, nil)

		require.NoError(t, svc.uninstall(uninstallInput{}))
		assert.False(t, gi.removed)
	})

	t.Run("no detected agents returns nil", func(t *testing.T) {
		svc := newMCPService([]agent.Agent{}, nil)
		require.NoError(t, svc.uninstall(uninstallInput{}))
	})
}
