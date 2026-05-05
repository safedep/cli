package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/safedep/dry/cloud"
	"github.com/safedep/dry/log"
)

// APIKeyInput is the data needed to persist an API-key credential.
type APIKeyInput struct {
	APIKey string
	Tenant string
}

// SaveAPIKey persists the API key + tenant to the provided keychain
// store. The store is expected to be already scoped to the active
// profile by the caller.
func SaveAPIKey(_ context.Context, store cloud.CredentialStore, in APIKeyInput) error {
	if in.APIKey == "" {
		return errors.New("auth: api key is required")
	}
	if in.Tenant == "" {
		return errors.New("auth: tenant is required")
	}
	if err := store.SaveAPIKeyCredential(in.APIKey, in.Tenant); err != nil {
		return fmt.Errorf("auth: save api key: %w", err)
	}
	return nil
}

// VerifyAPIKey checks that the supplied API key + tenant authenticate
// against the SafeDep data plane. We connect and issue a low-cost RPC. A
// successful round trip means the key is valid for that tenant.
func VerifyAPIKey(_ context.Context, in APIKeyInput) error {
	if in.APIKey == "" || in.Tenant == "" {
		return errors.New("auth: api key and tenant are required for verification")
	}

	creds, err := cloud.NewAPIKeyCredential(in.APIKey, in.Tenant)
	if err != nil {
		return fmt.Errorf("auth: build credentials: %w", err)
	}

	client, err := cloud.NewDataPlaneClient(GRPCAppName, creds)
	if err != nil {
		return fmt.Errorf("auth: data plane connect: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Warnf("auth: close data plane client: %v", err)
		}
	}()

	if err := pingDataPlane(client); err != nil {
		return fmt.Errorf("auth: data plane ping: %w", err)
	}
	return nil
}
