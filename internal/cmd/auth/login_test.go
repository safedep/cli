package auth

import (
	"testing"
	"time"

	"github.com/safedep/dry/cloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyPlan(t *testing.T) {
	tests := []struct {
		name        string
		flags       loginFlags
		wantCreate  bool
		wantDecider bool
	}{
		{name: "default checks the stored key", flags: loginFlags{}, wantCreate: true, wantDecider: true},
		{name: "--rotate-api-key always creates", flags: loginFlags{rotateAPIKey: true}, wantCreate: true},
		{name: "--no-api-key creates no key", flags: loginFlags{noAPIKey: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			create, decide := apiKeyPlan(tc.flags, nil)
			assert.Equal(t, tc.wantCreate, create)
			assert.Equal(t, tc.wantDecider, decide != nil)
		})
	}
}

// pmg and vet read the keychain key. An API key in the environment must not
// hide an expired keychain key from login.
func TestStoredAPIKeyIgnoresTheEnvironment(t *testing.T) {
	t.Setenv("SAFEDEP_API_KEY", "sk-env")
	t.Setenv("SAFEDEP_TENANT_ID", "acme.safedep.io")

	opts := []cloud.KeychainOption{
		cloud.WithAppName("safedep-cli-test-" + t.Name()),
		cloud.WithInsecureFileFallbackPath(t.TempDir() + "/creds.json"),
	}
	store, err := cloud.NewKeychainCredentialStore(opts...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	expired := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, store.(cloud.APIKeyMetadataStore).SaveAPIKeyCredentialWithMetadata(
		"sk-keychain", "acme.safedep.io", cloud.APIKeyMetadata{KeyID: "key-1", ExpiresAt: expired}))

	creds, err := storedAPIKey(opts)
	require.NoError(t, err)
	require.NotNil(t, creds)

	key, err := creds.GetAPIKey()
	require.NoError(t, err)
	assert.Equal(t, "sk-keychain", key)
	got, ok := creds.APIKeyExpiresAt()
	require.True(t, ok)
	assert.True(t, expired.Equal(got))
}

func TestStoredAPIKeyEmptyKeychain(t *testing.T) {
	opts := []cloud.KeychainOption{
		cloud.WithAppName("safedep-cli-test-" + t.Name()),
		cloud.WithInsecureFileFallbackPath(t.TempDir() + "/creds.json"),
	}
	creds, err := storedAPIKey(opts)
	require.NoError(t, err)
	assert.Nil(t, creds)
}

func TestValidateAPIKeyExpiryDays(t *testing.T) {
	assert.Error(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 14}))
	assert.NoError(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 15}))
	assert.NoError(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 1, noAPIKey: true}))
}
