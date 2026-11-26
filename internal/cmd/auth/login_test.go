package auth

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/safedep/cli/internal/app"
	cliauth "github.com/safedep/cli/internal/auth"
	"github.com/safedep/cli/internal/config"
	"github.com/safedep/dry/cloud"
	"github.com/safedep/dry/tui/output"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
			create, decide := apiKeyPlan(tc.flags, nil, nil)
			assert.Equal(t, tc.wantCreate, create)
			assert.Equal(t, tc.wantDecider, decide != nil)
		})
	}
}

func TestValidateAPIKeyExpiryDays(t *testing.T) {
	assert.Error(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 14}))
	assert.NoError(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 15}))
	assert.NoError(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 1, noAPIKey: true}))
	assert.NoError(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 7, rotateAPIKey: true}))
	assert.Error(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 0, rotateAPIKey: true}))
}

// TestDeviceLogin runs device login against a file keychain, a fake data
// plane and a bootstrap that calls the key check of login, so it covers what
// login hands the bootstrap, what it saves and what it tells the user.
func TestDeviceLogin(t *testing.T) {
	const tenant = "acme.safedep.io"
	now := time.Now().UTC().Truncate(time.Second)
	newExpiry := now.Add(90 * 24 * time.Hour)

	tests := []struct {
		name         string
		storedExpiry time.Time
		verifyErr    error
		createErr    error
		wantAPIKey   string
		wantVerified bool
		wantOutput   []string
	}{
		{
			name:         "replaces an expired stored key and saves the new one",
			storedExpiry: now.Add(-24 * time.Hour),
			wantAPIKey:   "sk-new",
			wantOutput:   []string{"Created a new API key: the stored API key expired on"},
		},
		{
			name:         "keeps a valid stored key and reports a failed check",
			storedExpiry: now.Add(60 * 24 * time.Hour),
			verifyErr:    status.Error(codes.Unavailable, "no route"),
			wantAPIKey:   "sk-old",
			wantVerified: true,
			wantOutput:   []string{"could not verify the stored API key", "Kept the stored API key"},
		},
		{
			name:         "replaces a key that SafeDep Cloud rejects",
			storedExpiry: now.Add(60 * 24 * time.Hour),
			verifyErr:    status.Error(codes.Unauthenticated, "key expired"),
			wantAPIKey:   "sk-new",
			wantVerified: true,
			wantOutput:   []string{"Created a new API key: SafeDep Cloud rejected the stored API key"},
		},
		{
			name:         "saves the OAuth tokens when no key can be created",
			storedExpiry: now.Add(-24 * time.Hour),
			createErr:    status.Error(codes.PermissionDenied, "member role required"),
			wantAPIKey:   "sk-old",
			wantOutput:   []string{"Could not create an API key", "No usable API key: the stored API key expired on"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := []cloud.KeychainOption{
				cloud.WithAppName("safedep-cli-test-" + t.Name()),
				cloud.WithInsecureFileFallbackPath(t.TempDir() + "/creds.json"),
			}
			store, err := cloud.NewKeychainCredentialStore(opts...)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			require.NoError(t, cliauth.SaveBootstrapResult(store, "access-old", "refresh-old", &cliauth.BootstrapResult{
				Tenant: tenant, APIKey: "sk-old", APIKeyID: "key-old", APIKeyExpiresAt: tc.storedExpiry,
			}))

			var verified cliauth.APIKeyInput
			deps := deviceLoginDeps{
				store:        func() (cloud.CredentialStore, error) { return store, nil },
				storedAPIKey: func() (*cloud.Credentials, error) { return cliauth.StoredAPIKey(opts) },
				deviceFlow: func(context.Context) (*cliauth.DeviceFlowResult, error) {
					return &cliauth.DeviceFlowResult{AccessToken: "access-new", RefreshToken: "refresh-new"}, nil
				},
				verify: func(_ context.Context, in cliauth.APIKeyInput) error {
					verified = in
					return tc.verifyErr
				},
				// The bootstrap of login after the tenant pick: it keeps the
				// stored key when the check says so, and creates a key
				// otherwise.
				bootstrap: func(ctx context.Context, in cliauth.BootstrapInput) (*cliauth.BootstrapResult, error) {
					res := &cliauth.BootstrapResult{Tenant: tenant}
					if in.DecideAPIKey != nil {
						res.APIKeyDecision = in.DecideAPIKey(ctx, tenant)
						if res.APIKeyDecision.Keep {
							return res, nil
						}
					}
					if tc.createErr != nil {
						res.APIKeyError = tc.createErr
						return res, nil
					}
					res.APIKey, res.APIKeyID, res.APIKeyExpiresAt = "sk-new", "key-new", newExpiry
					return res, nil
				},
			}

			var stderr bytes.Buffer
			output.SetWriters(io.Discard, &stderr)
			t.Cleanup(func() { output.SetWriters(os.Stdout, os.Stderr) })

			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			err = deviceLogin(cmd, app.New(&config.Config{}), loginFlags{tenant: tenant, apiKeyExpiryDays: 90}, deps)
			require.NoError(t, err)

			stored, err := cliauth.StoredAPIKey(opts)
			require.NoError(t, err)
			key, err := stored.GetAPIKey()
			require.NoError(t, err)
			assert.Equal(t, tc.wantAPIKey, key)
			if tc.wantAPIKey == "sk-new" {
				expiresAt, ok := stored.APIKeyExpiresAt()
				require.True(t, ok)
				assert.True(t, newExpiry.Equal(expiresAt))
			}

			tokens, err := cloud.NewKeychainCredentialResolver(cloud.CredentialTypeToken, opts...)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tokens.Close()) })
			creds, err := tokens.Resolve()
			require.NoError(t, err)
			token, err := creds.GetToken()
			require.NoError(t, err)
			assert.Equal(t, "access-new", token, "login must save the OAuth tokens")

			if tc.wantVerified {
				assert.Equal(t, cliauth.APIKeyInput{APIKey: "sk-old", Tenant: tenant}, verified)
			}
			for _, want := range tc.wantOutput {
				assert.Contains(t, stderr.String(), want)
			}
		})
	}
}
