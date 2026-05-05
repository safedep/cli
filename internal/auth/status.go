package auth

import (
	"context"
	"errors"
	"time"

	"github.com/safedep/dry/cloud"
	"github.com/safedep/dry/log"
)

// Status describes what credentials a profile currently holds.
type Status struct {
	Profile        string
	Tenant         string
	APIKey         bool
	OAuth          bool
	OAuthExpiresAt time.Time
}

// BuildStatus inspects the keychain via two resolvers (one per credential
// type) and returns what the active profile holds. Missing-credentials
// errors are treated as "not configured" rather than failures.
func BuildStatus(_ context.Context, profile string, opts []cloud.KeychainOption) (Status, error) {
	s := Status{Profile: profile}

	apiResolver, err := cloud.NewKeychainCredentialResolver(cloud.CredentialTypeAPIKey, opts...)
	if err != nil {
		return s, err
	}
	defer closeResolver(apiResolver)

	if creds, err := apiResolver.Resolve(); err == nil {
		s.APIKey = true
		if t, terr := creds.GetTenantDomain(); terr == nil {
			s.Tenant = t
		}
	} else if !isMissingCredentials(err) {
		return s, err
	}

	tokenResolver, err := cloud.NewKeychainCredentialResolver(cloud.CredentialTypeToken, opts...)
	if err != nil {
		return s, err
	}
	defer closeResolver(tokenResolver)

	if creds, err := tokenResolver.Resolve(); err == nil {
		s.OAuth = true
		if s.Tenant == "" {
			if t, terr := creds.GetTenantDomain(); terr == nil {
				s.Tenant = t
			}
		}
		if token, terr := creds.GetToken(); terr == nil {
			if exp, eerr := AccessTokenExpiry(token); eerr == nil {
				s.OAuthExpiresAt = exp
			}
		}
	} else if !isMissingCredentials(err) {
		return s, err
	}

	return s, nil
}

func isMissingCredentials(err error) bool {
	return errors.Is(err, cloud.ErrMissingCredentials)
}

func closeResolver(r cloud.CredentialResolver) {
	c, ok := r.(cloud.CloseableCredentialResolver)
	if !ok {
		return
	}
	if err := c.Close(); err != nil {
		log.Warnf("auth: close credential resolver: %v", err)
	}
}
