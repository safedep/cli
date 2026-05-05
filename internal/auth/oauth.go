// Package auth contains the SafeDep CLI's authentication flows. It owns
// the OAuth2 device-code login, the static API-key login, and the helpers
// that read and write credentials via dry/cloud's keychain.
//
// Commands under internal/cmd/auth invoke these flows. Nothing else in
// the CLI talks to the keychain directly.
package auth

import (
	"os"
	"time"
)

const (
	// CLIClientID is the SafeDep CLI's OAuth2 client. Override with the
	// SAFEDEP_CLI_CLIENT_ID env var for staging or local testing.
	cliClientID = "2ck5KJ9LyrPkzB8KANHSfNVpWJsQyC9d"

	cliAudience      = "https://cloud.safedep.io"
	cliDeviceCodeURL = "https://auth.safedep.io/oauth/device/code"
	cliTokenURL      = "https://auth.safedep.io/oauth/token" // #nosec G101 -- public OAuth endpoint URL.

	envClientID      = "SAFEDEP_CLI_CLIENT_ID"
	envAudience      = "SAFEDEP_CLI_AUDIENCE"
	envDeviceCodeURL = "SAFEDEP_CLI_DEVICE_CODE_URL"
	envTokenURL      = "SAFEDEP_CLI_TOKEN_URL" // #nosec G101 -- env var name for endpoint, not a credential.

	// DefaultAPIKeyExpiryDays is the lifetime of API keys created by the
	// device-code login flow. Override with --api-key-expiry-days.
	DefaultAPIKeyExpiryDays = 90

	// gRPCAppName identifies our connections to SafeDep Cloud in logs.
	GRPCAppName = "safedep-cli"
)

// CLIScopes are the OAuth scopes we request. offline_access is required
// to receive a refresh token.
var CLIScopes = []string{"offline_access", "openid", "profile", "email"}

// ClientID returns the OAuth client ID, honouring the env override.
func ClientID() string { return envOr(envClientID, cliClientID) }

// Audience returns the OAuth audience, honouring the env override.
func Audience() string { return envOr(envAudience, cliAudience) }

// DeviceCodeURL returns the device-code endpoint, honouring the env override.
func DeviceCodeURL() string { return envOr(envDeviceCodeURL, cliDeviceCodeURL) }

// TokenURL returns the token endpoint, honouring the env override.
func TokenURL() string { return envOr(envTokenURL, cliTokenURL) }

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// APIKeyName returns the human-readable name used when creating API keys
// from the device login flow. Stable enough for users to identify keys in
// the cloud UI. Unique enough to avoid collisions on repeated logins.
func APIKeyName(hostname string, now time.Time) string {
	if hostname == "" {
		hostname = "unknown-host"
	}
	return "safedep-cli@" + hostname + " (" + now.UTC().Format(time.RFC3339) + ")"
}
