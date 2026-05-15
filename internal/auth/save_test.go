package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/safedep/cli/internal/auth/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveBootstrapResult(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T) *mocks.CredentialStoreMock
		accessToken   string
		refreshToken  string
		bootstrap     *BootstrapResult
		expectErr     bool
		expectErrText string
	}{
		{
			name: "success with API key",
			setup: func(t *testing.T) *mocks.CredentialStoreMock {
				m := mocks.NewCredentialStoreMock(t)
				m.EXPECT().SaveTokenCredential("access-token-123", "refresh-token-456", "example.safedep.io").Return(nil)
				m.EXPECT().SaveAPIKeyCredential("api-key-789", "example.safedep.io").Return(nil)
				return m
			},
			accessToken:  "access-token-123",
			refreshToken: "refresh-token-456",
			bootstrap: &BootstrapResult{
				Tenant:          "example.safedep.io",
				APIKey:          "api-key-789",
				APIKeyExpiresAt: time.Now().Add(24 * time.Hour),
			},
			expectErr: false,
		},
		{
			name: "success without API key",
			setup: func(t *testing.T) *mocks.CredentialStoreMock {
				m := mocks.NewCredentialStoreMock(t)
				m.EXPECT().SaveTokenCredential("access-token-123", "refresh-token-456", "example.safedep.io").Return(nil)
				return m
			},
			accessToken:  "access-token-123",
			refreshToken: "refresh-token-456",
			bootstrap: &BootstrapResult{
				Tenant:          "example.safedep.io",
				APIKey:          "",
				APIKeyExpiresAt: time.Time{},
			},
			expectErr: false,
		},
		{
			name: "error on SaveTokenCredential",
			setup: func(t *testing.T) *mocks.CredentialStoreMock {
				m := mocks.NewCredentialStoreMock(t)
				m.EXPECT().SaveTokenCredential("access-token-123", "refresh-token-456", "example.safedep.io").Return(errors.New("keychain error"))
				return m
			},
			accessToken:  "access-token-123",
			refreshToken: "refresh-token-456",
			bootstrap: &BootstrapResult{
				Tenant: "example.safedep.io",
				APIKey: "api-key-789",
			},
			expectErr:     true,
			expectErrText: "save token credential:",
		},
		{
			name: "error on SaveAPIKeyCredential",
			setup: func(t *testing.T) *mocks.CredentialStoreMock {
				m := mocks.NewCredentialStoreMock(t)
				m.EXPECT().SaveTokenCredential("access-token-123", "refresh-token-456", "example.safedep.io").Return(nil)
				m.EXPECT().SaveAPIKeyCredential("api-key-789", "example.safedep.io").Return(errors.New("keychain error"))
				return m
			},
			accessToken:  "access-token-123",
			refreshToken: "refresh-token-456",
			bootstrap: &BootstrapResult{
				Tenant: "example.safedep.io",
				APIKey: "api-key-789",
			},
			expectErr:     true,
			expectErrText: "save api key credential:",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := tc.setup(t)
			err := SaveBootstrapResult(store, tc.accessToken, tc.refreshToken, tc.bootstrap)
			if tc.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectErrText)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
