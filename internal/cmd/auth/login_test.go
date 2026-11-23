package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPIKeyPlan(t *testing.T) {
	tests := []struct {
		name        string
		flags       loginFlags
		wantCreate  bool
		wantDecider bool
	}{
		{name: "default checks the stored key", flags: loginFlags{}, wantCreate: true, wantDecider: true},
		{name: "--rotate-api-key always creates", flags: loginFlags{rotateAPIKey: true}, wantCreate: true},
		{name: "--no-api-key creates no key", flags: loginFlags{noAPIKey: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			create, decide := apiKeyPlan(tc.flags, nil)
			assert.Equal(t, tc.wantCreate, create)
			assert.Equal(t, tc.wantDecider, decide != nil)
		})
	}
}

func TestValidateAPIKeyExpiryDays(t *testing.T) {
	assert.Error(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 14}))
	assert.NoError(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 15}))
	assert.NoError(t, validateAPIKeyExpiryDays(loginFlags{apiKeyExpiryDays: 1, noAPIKey: true}))
}
