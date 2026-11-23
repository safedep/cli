package auth

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliauth "github.com/safedep/cli/internal/auth"
)

var statusNow = time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

// keyValidUntil is an API key expiry outside the renewal window of statusNow.
var keyValidUntil = statusNow.Add(60 * 24 * time.Hour)

func TestNextStepHint(t *testing.T) {
	tests := []struct {
		name string
		st   cliauth.Status
		want string
	}{
		{"nothing configured", cliauth.Status{}, "safedep auth login"},
		{"api key only", cliauth.Status{APIKey: true}, "OAuth token missing"},
		{"oauth only", cliauth.Status{OAuth: true}, "API key missing"},
		{"fully authenticated", cliauth.Status{APIKey: true, OAuth: true, APIKeyExpiresAt: keyValidUntil}, ""},
		{
			"oauth expired",
			cliauth.Status{APIKey: true, OAuth: true, OAuthExpiresAt: statusNow.Add(-16 * 24 * time.Hour)},
			"OAuth token expired",
		},
		{
			"api key expired with a valid oauth token",
			cliauth.Status{APIKey: true, OAuth: true, APIKeyExpiresAt: statusNow.Add(-time.Hour)},
			"API key expired",
		},
		{
			"api key inside the renewal window",
			cliauth.Status{APIKey: true, OAuth: true, APIKeyExpiresAt: statusNow.Add(3 * 24 * time.Hour)},
			"API key expires soon",
		},
		{
			"api key with no recorded expiry",
			cliauth.Status{APIKey: true, OAuth: true},
			"API key expiry is not recorded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nextStepHint(tc.st, statusNow)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.want)
		})
	}
}

func TestOverallStatusBadge(t *testing.T) {
	tests := []struct {
		name string
		st   cliauth.Status
		want string
	}{
		{"both valid", cliauth.Status{APIKey: true, OAuth: true, OAuthExpiresAt: statusNow.Add(time.Hour)}, "authenticated"},
		{"no known expiry counts valid", cliauth.Status{APIKey: true, OAuth: true}, "authenticated"},
		{
			"api key expired",
			cliauth.Status{APIKey: true, OAuth: true, APIKeyExpiresAt: statusNow.Add(-time.Hour)},
			"partially authenticated",
		},
		{"oauth expired", cliauth.Status{APIKey: true, OAuth: true, OAuthExpiresAt: statusNow.Add(-time.Hour)}, "partially authenticated"},
		{"api key only", cliauth.Status{APIKey: true}, "partially authenticated"},
		{"nothing", cliauth.Status{}, "not authenticated"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, overallStatusBadge(tc.st, statusNow), tc.want)
		})
	}
}

func TestStatusResultRenderTable(t *testing.T) {
	r := &statusResult{st: cliauth.Status{
		Profile:         "default",
		Tenant:          "acme.safedep.io",
		APIKey:          true,
		OAuth:           true,
		APIKeyExpiresAt: time.Now().Add(60 * 24 * time.Hour),
	}}
	out := r.RenderTable()
	assert.Contains(t, out, "Authentication")
	assert.Contains(t, out, "authenticated")
	assert.Contains(t, out, "acme.safedep.io")
	assert.Contains(t, out, "API key expires")
	assert.NotContains(t, out, "auth login", "no hint when fully authenticated")
}

// An expired API key with a valid OAuth token is the state that made pmg
// fail while status still read "authenticated".
func TestStatusResultRenderTableExpiredAPIKey(t *testing.T) {
	r := &statusResult{st: cliauth.Status{
		Profile:         "default",
		Tenant:          "acme.safedep.io",
		APIKey:          true,
		OAuth:           true,
		OAuthExpiresAt:  time.Now().Add(9 * time.Hour),
		APIKeyExpiresAt: time.Now().Add(-48 * time.Hour),
	}}
	out := r.RenderTable()
	assert.Contains(t, out, "partially authenticated")
	assert.Contains(t, out, "API key expired")
	assert.Contains(t, out, "safedep auth login")
}

func TestStatusResultRenderJSONAPIKeyState(t *testing.T) {
	expiresAt := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	r := &statusResult{st: cliauth.Status{
		Profile:         "default",
		APIKey:          true,
		APIKeyExpiresAt: expiresAt,
	}}
	data, err := r.RenderJSON()
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "expired", got["api_key_state"])
	assert.Equal(t, expiresAt.Format(time.RFC3339), got["api_key_expires_at"])
}

func TestStatusResultRenderTableExpiredOAuth(t *testing.T) {
	r := &statusResult{st: cliauth.Status{
		Profile:        "default",
		Tenant:         "acme.safedep.io",
		APIKey:         true,
		OAuth:          true,
		OAuthExpiresAt: time.Now().Add(-16 * 24 * time.Hour),
	}}
	out := r.RenderTable()
	assert.Contains(t, out, "partially authenticated")
	assert.Contains(t, out, "expired")
	assert.Contains(t, out, "OAuth expired")
	assert.NotContains(t, out, "OAuth expires")
	assert.Contains(t, out, "safedep auth login")
}

func TestStatusResultRenderTableUnauthenticatedHasHint(t *testing.T) {
	r := &statusResult{st: cliauth.Status{Profile: "default"}}
	out := r.RenderTable()
	assert.Contains(t, out, "not authenticated")
	assert.Contains(t, out, "safedep auth login")
}

func TestStatusResultRenderPlainUnchangedShape(t *testing.T) {
	r := &statusResult{st: cliauth.Status{
		Profile:        "default",
		APIKey:         true,
		OAuth:          true,
		OAuthExpiresAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}}
	plain := r.RenderPlain()
	lines := strings.Split(plain, "\n")
	assert.Equal(t, "Profile:        default", lines[0])
	assert.Contains(t, plain, "OAuth expires:  2026-08-01T00:00:00Z")
	assert.NotContains(t, plain, "Status:", "plain output must not gain the table-only status row")
}
