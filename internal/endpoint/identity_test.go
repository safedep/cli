package endpoint

import (
	"encoding/base64"
	"errors"
	"testing"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type fakeResolver struct {
	identity *controltowerv1.EndpointIdentity
	err      error
}

func (f *fakeResolver) Resolve() (*controltowerv1.EndpointIdentity, error) {
	return f.identity, f.err
}

func TestBuildMCPConfig(t *testing.T) {
	identity := &controltowerv1.EndpointIdentity{
		Identifier: "my-machine",
		MachineId:  "abc123",
	}

	t.Run("populates all three headers", func(t *testing.T) {
		resolver := &fakeResolver{identity: identity}

		cfg, err := BuildMCPConfig("https://mcp.safedep.io/", "key123", "tenant-1", resolver)
		require.NoError(t, err)

		assert.Equal(t, "https://mcp.safedep.io/", cfg.URL)
		assert.Equal(t, "Bearer key123", cfg.Headers["Authorization"])
		assert.Equal(t, "tenant-1", cfg.Headers["X-Tenant-ID"])
		assert.NotEmpty(t, cfg.Headers["X-Endpoint-ID"])
	})

	t.Run("X-Endpoint-ID is valid base64 that decodes to the identity", func(t *testing.T) {
		resolver := &fakeResolver{identity: identity}

		cfg, err := BuildMCPConfig("https://mcp.safedep.io/", "k", "t", resolver)
		require.NoError(t, err)

		raw, err := base64.StdEncoding.DecodeString(cfg.Headers["X-Endpoint-ID"])
		require.NoError(t, err)

		var decoded controltowerv1.EndpointIdentity
		require.NoError(t, proto.Unmarshal(raw, &decoded))
		assert.Equal(t, identity.Identifier, decoded.Identifier)
		assert.Equal(t, identity.MachineId, decoded.MachineId)
	})

	t.Run("returns error when resolver fails", func(t *testing.T) {
		resolver := &fakeResolver{err: errors.New("no machine id")}
		_, err := BuildMCPConfig("https://mcp.safedep.io/", "k", "t", resolver)
		require.Error(t, err)
	})
}

func TestIdentityHeaderValue(t *testing.T) {
	t.Run("returns base64-encoded proto-marshaled identity", func(t *testing.T) {
		identity := &controltowerv1.EndpointIdentity{
			Identifier: "my-machine",
			MachineId:  "abc123",
		}
		resolver := &fakeResolver{identity: identity}

		got, err := IdentityHeaderValue(resolver)
		require.NoError(t, err)

		// Decode and verify round-trip.
		raw, err := base64.StdEncoding.DecodeString(got)
		require.NoError(t, err)

		var decoded controltowerv1.EndpointIdentity
		require.NoError(t, proto.Unmarshal(raw, &decoded))
		assert.Equal(t, identity.Identifier, decoded.Identifier)
		assert.Equal(t, identity.MachineId, decoded.MachineId)
	})

	t.Run("returns error when resolver fails", func(t *testing.T) {
		resolver := &fakeResolver{err: errors.New("no machine id")}
		_, err := IdentityHeaderValue(resolver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolve identity")
	})

	t.Run("produces stable output for same identity", func(t *testing.T) {
		identity := &controltowerv1.EndpointIdentity{
			Identifier: "stable-host",
			MachineId:  "xyz789",
		}
		resolver := &fakeResolver{identity: identity}

		a, err := IdentityHeaderValue(resolver)
		require.NoError(t, err)
		b, err := IdentityHeaderValue(resolver)
		require.NoError(t, err)

		assert.Equal(t, a, b)
	})
}
