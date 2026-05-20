package auth

import (
	"fmt"

	"github.com/safedep/dry/cloud"
)

// SaveBootstrapResult persists the access token, refresh token, and (if present)
// API key from a completed bootstrap to the credential store.
func SaveBootstrapResult(store cloud.CredentialStore, accessToken, refreshToken string, b *BootstrapResult) error {
	if err := store.SaveTokenCredential(accessToken, refreshToken, b.Tenant); err != nil {
		return fmt.Errorf("save token credential: %w", err)
	}

	if b.APIKey != "" {
		if err := store.SaveAPIKeyCredential(b.APIKey, b.Tenant); err != nil {
			return fmt.Errorf("save api key credential: %w", err)
		}
	}

	return nil
}
