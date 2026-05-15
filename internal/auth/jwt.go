package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessTokenExpiry decodes the unverified `exp` claim of a JWT and returns
// it as a UTC time. Verification is the identity provider's job. We only
// need the expiry to drive UI hints and the "session expired" error path.
func AccessTokenExpiry(token string) (time.Time, error) {
	if token == "" {
		return time.Time{}, errors.New("auth: empty access token")
	}
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := jwt.MapClaims{}
	if _, _, err := parser.ParseUnverified(token, claims); err != nil {
		return time.Time{}, err
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return time.Time{}, errors.New("auth: token has no exp claim")
	}
	return exp.UTC(), nil
}

// IsExpired reports whether the token's exp claim is in the past. Tokens
// without a parseable exp claim are treated as expired so callers fall
// through to a re-login path.
func IsExpired(token string, now time.Time) bool {
	exp, err := AccessTokenExpiry(token)
	if err != nil {
		return true
	}
	return !now.Before(exp)
}

// EmailFromAccessToken extracts the email claim from an unverified JWT access
// token. It tries the standard "email" claim first, then the namespaced
// "https://safedep.io/email" claim. Returns empty string if neither is present.
func EmailFromAccessToken(token string) string {
	if token == "" {
		return ""
	}
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := jwt.MapClaims{}
	if _, _, err := parser.ParseUnverified(token, claims); err != nil {
		return ""
	}

	// Try standard email claim first.
	if email, ok := claims["email"].(string); ok && email != "" {
		return email
	}

	// Try namespaced claim.
	if email, ok := claims["https://safedep.io/email"].(string); ok && email != "" {
		return email
	}

	return ""
}
