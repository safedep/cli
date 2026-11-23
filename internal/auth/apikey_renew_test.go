package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/safedep/dry/cloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// keychainCredentials saves an API key the way login does and resolves it the
// way pmg and auth status do, so the test covers what the keychain holds.
func keychainCredentials(t *testing.T, key, tenant string, meta cloud.APIKeyMetadata) *cloud.Credentials {
	t.Helper()
	opts := []cloud.KeychainOption{
		cloud.WithAppName("safedep-cli-test-" + t.Name()),
		cloud.WithInsecureFileFallbackPath(t.TempDir() + "/creds.json"),
	}

	store, err := cloud.NewKeychainCredentialStore(opts...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, SaveBootstrapResult(store, "access", "refresh", &BootstrapResult{
		Tenant: tenant, APIKey: key, APIKeyID: meta.KeyID, APIKeyExpiresAt: meta.ExpiresAt,
	}))

	resolver, err := cloud.NewKeychainCredentialResolver(cloud.CredentialTypeAPIKey, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resolver.Close()) })

	creds, err := resolver.Resolve()
	require.NoError(t, err)
	return creds
}

func TestDecideStoredAPIKey(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	valid := cloud.APIKeyMetadata{KeyID: "key-1", ExpiresAt: now.Add(60 * 24 * time.Hour)}

	tests := []struct {
		name string
		// stored is nil for an empty keychain.
		stored       func(t *testing.T) *cloud.Credentials
		verifyErr    error
		wantKeep     bool
		wantReason   string
		wantVerified bool
		wantWarning  bool
		wantFallback bool
	}{
		{
			name:       "no stored key",
			stored:     func(*testing.T) *cloud.Credentials { return nil },
			wantReason: "no API key is stored",
		},
		{
			name: "a key of another tenant",
			stored: func(t *testing.T) *cloud.Credentials {
				return keychainCredentials(t, "sk-1", "old.safedep.io", valid)
			},
			wantReason: `belongs to tenant "old.safedep.io"`,
		},
		{
			name: "an expired key",
			stored: func(t *testing.T) *cloud.Credentials {
				return keychainCredentials(t, "sk-1", "acme.safedep.io",
					cloud.APIKeyMetadata{KeyID: "key-1", ExpiresAt: now.Add(-24 * time.Hour)})
			},
			wantReason: "expired on 2026-11-21",
		},
		{
			name: "a key inside the renewal window",
			stored: func(t *testing.T) *cloud.Credentials {
				return keychainCredentials(t, "sk-1", "acme.safedep.io",
					cloud.APIKeyMetadata{KeyID: "key-1", ExpiresAt: now.Add(3 * 24 * time.Hour)})
			},
			wantReason:   "expires on 2026-11-25",
			wantFallback: true,
		},
		{
			name: "a key with no recorded expiry that SafeDep Cloud accepts",
			stored: func(t *testing.T) *cloud.Credentials {
				return keychainCredentials(t, "sk-1", "acme.safedep.io", cloud.APIKeyMetadata{})
			},
			wantKeep:     true,
			wantVerified: true,
		},
		{
			name: "a key with no recorded expiry that SafeDep Cloud rejects",
			stored: func(t *testing.T) *cloud.Credentials {
				return keychainCredentials(t, "sk-1", "acme.safedep.io", cloud.APIKeyMetadata{})
			},
			verifyErr:    fmt.Errorf("auth: data plane ping: %w", status.Error(codes.Unauthenticated, "key expired")),
			wantReason:   "SafeDep Cloud rejected the stored API key",
			wantVerified: true,
		},
		{
			name: "a valid key that SafeDep Cloud accepts",
			stored: func(t *testing.T) *cloud.Credentials {
				return keychainCredentials(t, "sk-1", "acme.safedep.io", valid)
			},
			wantKeep:     true,
			wantVerified: true,
		},
		{
			name: "a valid key that SafeDep Cloud rejects",
			stored: func(t *testing.T) *cloud.Credentials {
				return keychainCredentials(t, "sk-1", "acme.safedep.io", valid)
			},
			verifyErr:    fmt.Errorf("auth: data plane ping: %w", status.Error(codes.Unauthenticated, "key expired")),
			wantReason:   "SafeDep Cloud rejected the stored API key",
			wantVerified: true,
		},
		{
			name: "a valid key that the gateway does not know",
			stored: func(t *testing.T) *cloud.Credentials {
				return keychainCredentials(t, "sk-1", "acme.safedep.io", valid)
			},
			verifyErr:    status.Error(codes.PermissionDenied, "access denied"),
			wantReason:   "SafeDep Cloud rejected the stored API key",
			wantVerified: true,
		},
		{
			name: "a network failure keeps the key and warns",
			stored: func(t *testing.T) *cloud.Credentials {
				return keychainCredentials(t, "sk-1", "acme.safedep.io", valid)
			},
			verifyErr:    fmt.Errorf("auth: data plane ping: %w", status.Error(codes.Unavailable, "no route")),
			wantKeep:     true,
			wantVerified: true,
			wantWarning:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var verified APIKeyInput
			verify := func(_ context.Context, in APIKeyInput) error {
				verified = in
				return tc.verifyErr
			}

			got := DecideStoredAPIKey(context.Background(), tc.stored(t), "acme.safedep.io", now, verify)

			assert.Equal(t, tc.wantKeep, got.Keep)
			if tc.wantReason == "" {
				assert.Empty(t, got.Reason)
			} else {
				assert.Contains(t, got.Reason, tc.wantReason)
			}
			assert.Equal(t, tc.wantWarning, got.Warning != "", "warning %q", got.Warning)
			assert.Equal(t, tc.wantFallback, got.Fallback)
			if tc.wantVerified {
				assert.Equal(t, APIKeyInput{APIKey: "sk-1", Tenant: "acme.safedep.io"}, verified)
			} else {
				assert.Empty(t, verified, "the key must not be sent to SafeDep Cloud")
			}
			if got.Keep {
				if exp, ok := tc.stored(t).APIKeyExpiresAt(); ok {
					assert.True(t, exp.Equal(got.ExpiresAt))
				} else {
					assert.True(t, got.ExpiresAt.IsZero())
				}
			}
		})
	}
}

func TestBuildStatusReadsAPIKeyExpiry(t *testing.T) {
	expiresAt := time.Date(2027, 2, 20, 0, 0, 0, 0, time.UTC)
	opts := []cloud.KeychainOption{
		cloud.WithAppName("safedep-cli-test-" + t.Name()),
		cloud.WithInsecureFileFallbackPath(t.TempDir() + "/creds.json"),
	}
	store, err := cloud.NewKeychainCredentialStore(opts...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, SaveBootstrapResult(store, "access", "refresh", &BootstrapResult{
		Tenant: "acme.safedep.io", APIKey: "sk-1", APIKeyID: "key-1", APIKeyExpiresAt: expiresAt,
	}))

	st, err := BuildStatus(context.Background(), "default", opts)
	require.NoError(t, err)

	assert.True(t, st.APIKey)
	assert.True(t, expiresAt.Equal(st.APIKeyExpiresAt), "expiry %v", st.APIKeyExpiresAt)
}
