package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safedep/dry/cloud"
	"github.com/safedep/dry/log"
	"golang.org/x/oauth2"
)

// ErrRefreshFailed indicates the refresh token is expired, revoked, or
// otherwise invalid. Callers should prompt the user to re-authenticate.
var ErrRefreshFailed = errors.New("auth: refresh token invalid or expired: run `safedep auth login` to re-authenticate")

// RefreshAndPersistIfExpired checks whether creds contain an expired token and,
// if so, silently refreshes it using the stored refresh token, persists the new
// tokens, and returns fresh credentials. Returns creds unchanged when not expired.
func RefreshAndPersistIfExpired(ctx context.Context, store cloud.CredentialStore, creds *cloud.Credentials, keychainOpts []cloud.KeychainOption) (*cloud.Credentials, error) {
	token, err := creds.GetToken()
	if err != nil || !IsExpired(token, time.Now()) {
		return creds, nil
	}

	refreshToken, err := creds.GetRefreshToken()
	if err != nil || refreshToken == "" {
		return nil, errors.New("session expired: run `safedep auth login` to re-authenticate")
	}

	tenant, err := creds.GetTenantDomain()
	if err != nil {
		return nil, fmt.Errorf("auth: refresh: get tenant domain: %w", err)
	}

	log.Warnf("auth: access token expired for %q; attempting silent refresh", tenant)

	result, err := RefreshTokens(ctx, token, refreshToken)
	if err != nil {
		return nil, err
	}

	if err := store.SaveTokenCredential(result.AccessToken, result.RefreshToken, tenant); err != nil {
		return nil, fmt.Errorf("auth: refresh: save token: %w", err)
	}

	// Re-resolve from freshly-written keychain entry.
	r, err := cloud.NewKeychainCredentialResolver(cloud.CredentialTypeToken, keychainOpts...)
	if err != nil {
		return nil, fmt.Errorf("auth: refresh: resolver: %w", err)
	}

	return r.Resolve()
}

// RefreshTokens exchanges a refresh token for a fresh access + refresh token
// pair using golang.org/x/oauth2. Returns ErrRefreshFailed when the server
// rejects the token; callers should direct the user to re-login.
func RefreshTokens(ctx context.Context, accessToken, refreshToken string) (*DeviceFlowResult, error) {
	if refreshToken == "" {
		return nil, ErrRefreshFailed
	}

	// AccessTokenExpiry returns zero time on error, which is fine: a zero
	// Expiry on oauth2.Token is treated as already expired, so Token() will
	// still attempt the refresh.
	expiry, _ := AccessTokenExpiry(accessToken)

	cfg := &oauth2.Config{
		ClientID: ClientID(),
		Endpoint: oauth2.Endpoint{
			TokenURL:  TokenURL(),
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}

	t := &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       expiry,
	}

	fresh, err := cfg.TokenSource(ctx, t).Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRefreshFailed, err)
	}

	return &DeviceFlowResult{
		AccessToken:  fresh.AccessToken,
		RefreshToken: fresh.RefreshToken,
	}, nil
}
