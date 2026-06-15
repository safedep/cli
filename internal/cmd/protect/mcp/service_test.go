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
	injected      *agent.MCPConfig
	removed       bool
	injectErr     error
	configured    bool
	configuredErr error
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
func (f *fakeGlobalInjector) GlobalConfigured() (bool, error) {
	return f.configured, f.configuredErr
}

type fakeWorkspaceInjector struct {
	injected      *agent.MCPConfig
	removed       bool
	injectErr     error
	configured    bool
	configuredErr error
}

func (f *fakeWorkspaceInjector) WorkspaceConfigPath(_ string) string { return "/fake/ws/path" }
func (f *fakeWorkspaceInjector) InjectWorkspace(_ string, cfg agent.MCPConfig) error {
	if f.injectErr != nil {
		return f.injectErr
	}
	c := cfg
	f.injected = &c
	return nil
}
func (f *fakeWorkspaceInjector) RemoveWorkspace(_ string) error {
	f.removed = true
	return nil
}
func (f *fakeWorkspaceInjector) WorkspaceConfigured(_ string) (bool, error) {
	return f.configured, f.configuredErr
}

type fakeAgent struct {
	name      string
	detected  bool
	global    *fakeGlobalInjector
	workspace *fakeWorkspaceInjector
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
	if f.workspace == nil {
		return nil, false
	}
	return f.workspace, true
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

	t.Run("workspace-only agent is skipped when no workspaceDir", func(t *testing.T) {
		wi := &fakeWorkspaceInjector{}
		a := &fakeAgent{name: "vscode", detected: true, workspace: wi}
		svc := newMCPService([]agent.Agent{a}, &fakeResolver{identity: testIdentity})

		require.NoError(t, svc.install(installInput{
			MCPURL:       "https://mcp.safedep.io/v1",
			APIKey:       "k",
			TenantID:     "t",
			WorkspaceDir: "", // no workspace
		}))
		assert.Nil(t, wi.injected, "workspace-only agent must not be injected without --workspace")
	})

	t.Run("workspace-only agent is configured when workspaceDir is set", func(t *testing.T) {
		wi := &fakeWorkspaceInjector{}
		a := &fakeAgent{name: "vscode", detected: true, workspace: wi}
		svc := newMCPService([]agent.Agent{a}, &fakeResolver{identity: testIdentity})

		require.NoError(t, svc.install(installInput{
			MCPURL:       "https://mcp.safedep.io/v1",
			APIKey:       "k",
			TenantID:     "t",
			WorkspaceDir: "/project",
		}))
		require.NotNil(t, wi.injected)
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

func TestMCPServiceStatus(t *testing.T) {
	t.Run("reports detected and configured per agent", func(t *testing.T) {
		configured := &fakeAgent{name: "claude-code", detected: true, global: &fakeGlobalInjector{configured: true}}
		notConfigured := &fakeAgent{name: "cursor", detected: true, global: &fakeGlobalInjector{configured: false}}
		undetected := &fakeAgent{name: "vscode", detected: false, global: &fakeGlobalInjector{configured: true}}
		svc := newMCPService([]agent.Agent{configured, notConfigured, undetected}, nil)

		statuses, err := svc.status(statusInput{})
		require.NoError(t, err)
		require.Len(t, statuses, 3)

		assert.True(t, statuses[0].Detected)
		assert.True(t, statuses[0].Global.Supported)
		assert.True(t, statuses[0].Global.Configured)

		assert.True(t, statuses[1].Detected)
		assert.False(t, statuses[1].Global.Configured)

		// Undetected agents must not be probed for configuration.
		assert.False(t, statuses[2].Detected)
		assert.False(t, statuses[2].Global.Configured)
	})

	t.Run("skips workspace scope when no workspaceDir", func(t *testing.T) {
		a := &fakeAgent{name: "vscode", detected: true, workspace: &fakeWorkspaceInjector{configured: true}}
		svc := newMCPService([]agent.Agent{a}, nil)

		statuses, err := svc.status(statusInput{})
		require.NoError(t, err)
		require.Len(t, statuses, 1)
		assert.False(t, statuses[0].Workspace.Supported)
	})

	t.Run("reports workspace scope when workspaceDir set", func(t *testing.T) {
		a := &fakeAgent{name: "vscode", detected: true, workspace: &fakeWorkspaceInjector{configured: true}}
		svc := newMCPService([]agent.Agent{a}, nil)

		statuses, err := svc.status(statusInput{WorkspaceDir: "/project"})
		require.NoError(t, err)
		require.Len(t, statuses, 1)
		assert.True(t, statuses[0].Workspace.Supported)
		assert.True(t, statuses[0].Workspace.Configured)
	})

	t.Run("accumulates read errors", func(t *testing.T) {
		a := &fakeAgent{name: "claude-code", detected: true, global: &fakeGlobalInjector{configuredErr: errors.New("read fail")}}
		svc := newMCPService([]agent.Agent{a}, nil)

		statuses, err := svc.status(statusInput{})
		require.Error(t, err)
		require.Len(t, statuses, 1)
	})

	t.Run("returns partial report alongside per-scope error", func(t *testing.T) {
		failing := &fakeAgent{name: "claude-code", detected: true, global: &fakeGlobalInjector{configuredErr: errors.New("read fail")}}
		healthy := &fakeAgent{name: "cursor", detected: true, global: &fakeGlobalInjector{configured: true}}
		svc := newMCPService([]agent.Agent{failing, healthy}, nil)

		statuses, err := svc.status(statusInput{})
		require.Error(t, err)
		require.Len(t, statuses, 2)

		// The failing scope carries its error and is not reported as a clean
		// "not configured".
		require.Error(t, statuses[0].Global.Err)
		assert.False(t, statuses[0].Global.Configured)

		// The healthy agent is still reported.
		assert.NoError(t, statuses[1].Global.Err)
		assert.True(t, statuses[1].Global.Configured)
	})
}
