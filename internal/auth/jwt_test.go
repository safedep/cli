package auth

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailFromAccessToken(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected string
	}{
		{
			name:     "empty string input",
			token:    "",
			expected: "",
		},
		{
			name:     "malformed token",
			token:    "not.a.token",
			expected: "",
		},
		{
			name:     "standard email claim",
			token:    createTestToken(map[string]any{"email": "user@example.com"}),
			expected: "user@example.com",
		},
		{
			name:     "namespaced email claim",
			token:    createTestToken(map[string]any{"https://safedep.io/email": "namespaced@example.com"}),
			expected: "namespaced@example.com",
		},
		{
			name: "both claims present - standard wins",
			token: createTestToken(map[string]any{
				"email":                    "standard@example.com",
				"https://safedep.io/email": "namespaced@example.com",
			}),
			expected: "standard@example.com",
		},
		{
			name:     "no email claim",
			token:    createTestToken(map[string]any{"sub": "user123"}),
			expected: "",
		},
		{
			name:     "email claim with empty string",
			token:    createTestToken(map[string]any{"email": ""}),
			expected: "",
		},
		{
			name:     "namespaced email with empty string",
			token:    createTestToken(map[string]any{"https://safedep.io/email": ""}),
			expected: "",
		},
		{
			name:     "email claim is not a string",
			token:    createTestToken(map[string]any{"email": 123}),
			expected: "",
		},
		{
			name:     "namespaced email claim is not a string",
			token:    createTestToken(map[string]any{"https://safedep.io/email": []string{"email@example.com"}}),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EmailFromAccessToken(tt.token)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// createTestToken creates an unsigned JWT token with the given claims for testing.
func createTestToken(claims map[string]any) string {
	header := map[string]any{
		"alg": "none",
		"typ": "JWT",
	}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	// Unsigned token (empty signature).
	return headerB64 + "." + claimsB64 + "."
}
