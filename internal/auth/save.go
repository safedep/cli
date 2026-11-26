package auth

import (
	"fmt"

	"github.com/safedep/dry/cloud"
)

// SaveBootstrapResult persists the access token, refresh token, and (if present)
// API key from a completed bootstrap to the credential store. The key is saved
// with its id and expiry, so that the next login and auth status can tell an
// expired key from a valid one.
func SaveBootstrapResult(store cloud.CredentialStore, accessToken, refreshToken string, b *BootstrapResult) error {
	if err := store.SaveTokenCredential(accessToken, refreshToken, b.Tenant); err != nil {
		return fmt.Errorf("save token credential: %w", err)
	}

	if b.APIKey == "" {
		return nil
	}

	err := store.SaveAPIKeyCredentialWithMetadata(b.APIKey, b.Tenant, cloud.APIKeyMetadata{
		KeyID:     b.APIKeyID,
		ExpiresAt: b.APIKeyExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("save api key credential: %w", err)
	}

	return nil
}
