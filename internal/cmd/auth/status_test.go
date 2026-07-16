package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	cliauth "github.com/safedep/cli/internal/auth"
)

var statusNow = time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

func TestNextStepHint(t *testing.T) {
	tests := []struct {
		name string
		st   cliauth.Status
		want string
	}{
		{"nothing configured", cliauth.Status{}, "safedep auth login"},
		{"api key only", cliauth.Status{APIKey: true}, "OAuth token missing"},
		{"oauth only", cliauth.Status{OAuth: true}, "API key missing"},
		{"fully authenticated", cliauth.Status{APIKey: true, OAuth: true}, ""},
		{
			"oauth expired",
			cliauth.Status{APIKey: true, OAuth: true, OAuthExpiresAt: statusNow.Add(-16 * 24 * time.Hour)},
			"OAuth token expired",
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
		Profile: "default",
		Tenant:  "acme.safedep.io",
		APIKey:  true,
		OAuth:   true,
	}}
	out := r.RenderTable()
	assert.Contains(t, out, "Authentication")
	assert.Contains(t, out, "authenticated")
	assert.Contains(t, out, "acme.safedep.io")
	assert.NotContains(t, out, "auth login", "no hint when fully authenticated")
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
