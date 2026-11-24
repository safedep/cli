package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safedep/dry/cloud"
	"github.com/safedep/dry/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// APIKeyRenewWindow is how long before its expiry login replaces an API key.
// A key that expires inside the window would stop pmg and vet before the user
// signs in again.
const APIKeyRenewWindow = 14 * 24 * time.Hour

// APIKeyState is what the keychain says about the stored API key.
type APIKeyState int

const (
	APIKeyMissing APIKeyState = iota
	APIKeyValid
	APIKeyExpiring
	APIKeyExpired
	// APIKeyExpiryUnknown is a key saved with no expiry: a pasted key, or a
	// key from a CLI that did not record it. Only SafeDep Cloud can tell
	// whether it is valid.
	APIKeyExpiryUnknown
)

// APIKeyStateAt returns the state of a stored key at now.
func APIKeyStateAt(present bool, expiresAt time.Time, now time.Time) APIKeyState {
	switch {
	case !present:
		return APIKeyMissing
	case expiresAt.IsZero():
		return APIKeyExpiryUnknown
	case !expiresAt.After(now):
		return APIKeyExpired
	case expiresAt.Before(now.Add(APIKeyRenewWindow)):
		return APIKeyExpiring
	default:
		return APIKeyValid
	}
}

// APIKeyVerifier checks a key against SafeDep Cloud. VerifyAPIKey is the
// production verifier. Tests pass a fake.
type APIKeyVerifier func(ctx context.Context, in APIKeyInput) error

// APIKeyDecision is whether login keeps the stored API key, and why not.
type APIKeyDecision struct {
	Keep bool

	// Reason says why login replaces the key. Empty when Keep is true.
	Reason string

	// ExpiresAt is the expiry of the key that login keeps.
	ExpiresAt time.Time

	// Warning reports a check that could not finish. Login keeps the key,
	// because a network failure says nothing about the key.
	Warning string

	// Fallback means that the stored key still works for the tenant, so
	// login keeps it when it cannot create a new key. ExpiresAt is zero when
	// the expiry of the key is not recorded.
	Fallback bool
}

// DecideStoredAPIKey decides whether login keeps the API key that the
// keychain holds for the profile. stored is nil when the keychain holds no
// key. The key stays only when it belongs to tenant, its recorded expiry is
// more than APIKeyRenewWindow away, and SafeDep Cloud accepts it.
//
// A key with no recorded expiry is replaced: every key that a CLI saved
// before it recorded the expiry is one, and status cannot warn before such a
// key expires. Like a key inside the window, it is the fallback when login
// cannot create a new key.
func DecideStoredAPIKey(ctx context.Context, stored *cloud.Credentials, tenant string, now time.Time,
	verify APIKeyVerifier,
) APIKeyDecision {
	if stored == nil {
		return APIKeyDecision{Reason: "no API key is stored"}
	}

	storedTenant, err := stored.GetTenantDomain()
	if err != nil || storedTenant != tenant {
		return APIKeyDecision{Reason: fmt.Sprintf("the stored API key belongs to tenant %q", storedTenant)}
	}

	expiresAt, _ := stored.APIKeyExpiresAt()
	state := APIKeyStateAt(true, expiresAt, now)
	if state == APIKeyExpired {
		return APIKeyDecision{Reason: "the stored API key expired on " + formatDay(expiresAt)}
	}

	key, err := stored.GetAPIKey()
	if err != nil {
		return APIKeyDecision{Reason: "the stored API key cannot be read"}
	}

	// A key that login replaces is checked too: login falls back to it when
	// it cannot create a new key, and a revoked key must not be kept.
	warning := ""
	if err := verify(ctx, APIKeyInput{APIKey: key, Tenant: tenant}); err != nil {
		// The gateway answers Unauthenticated for an expired key and
		// PermissionDenied for a key that it does not know, such as a
		// revoked one. Both mean that a new key is needed.
		if code := status.Code(err); code == codes.Unauthenticated || code == codes.PermissionDenied {
			return APIKeyDecision{Reason: "SafeDep Cloud rejected the stored API key"}
		}
		warning = fmt.Sprintf("could not verify the stored API key: %v", err)
	}

	switch state {
	case APIKeyExpiring:
		return APIKeyDecision{Reason: "the stored API key expires on " + formatDay(expiresAt),
			Fallback: true, ExpiresAt: expiresAt, Warning: warning}
	case APIKeyExpiryUnknown:
		return APIKeyDecision{Reason: "the expiry of the stored API key is not recorded",
			Fallback: true, Warning: warning}
	}

	return APIKeyDecision{Keep: true, ExpiresAt: expiresAt, Warning: warning}
}

func formatDay(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// StoredAPIKey returns the API key credentials of the profile in the
// keychain, or nil when there are none. It reads the keychain alone: pmg and
// vet read the key from there, so an API key in the environment must not
// stop login from replacing an expired keychain key.
func StoredAPIKey(opts []cloud.KeychainOption) (*cloud.Credentials, error) {
	resolver, err := cloud.NewKeychainCredentialResolver(cloud.CredentialTypeAPIKey, opts...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resolver.Close(); err != nil {
			log.Warnf("auth: close keychain resolver: %v", err)
		}
	}()

	creds, err := resolver.Resolve()
	if errors.Is(err, cloud.ErrMissingCredentials) && !errors.Is(err, cloud.ErrIncompleteCredentials) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return creds, nil
}
