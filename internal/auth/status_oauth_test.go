package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStatusOAuthValid(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		st   Status
		want bool
	}{
		{"no token", Status{}, false},
		{"token without known expiry", Status{OAuth: true}, true},
		{"token expiring in future", Status{OAuth: true, OAuthExpiresAt: now.Add(time.Hour)}, true},
		{"token expired", Status{OAuth: true, OAuthExpiresAt: now.Add(-time.Hour)}, false},
		{"expiry set but no token", Status{OAuthExpiresAt: now.Add(time.Hour)}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.st.OAuthValid(now))
		})
	}
}
