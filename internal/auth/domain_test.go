package auth

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTenantDomain(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "simple ascii",
			input:    "Acme Corp",
			expected: "acme-corp",
		},
		{
			name:     "lowercase already",
			input:    "acme-corp",
			expected: "acme-corp",
		},
		{
			name:     "multiple spaces",
			input:    "Acme    Corp",
			expected: "acme-corp",
		},
		{
			name:     "special characters",
			input:    "Acme & Co.",
			expected: "acme-co",
		},
		{
			name:     "underscores to hyphens",
			input:    "acme_corp",
			expected: "acme-corp",
		},
		{
			name:     "mixed special chars",
			input:    "Acme@Corp!Inc",
			expected: "acme-corp-inc",
		},
		{
			name:     "leading/trailing spaces",
			input:    "  Acme Corp  ",
			expected: "acme-corp",
		},
		{
			name:     "leading/trailing hyphens (from special chars)",
			input:    "!Acme Corp!",
			expected: "acme-corp",
		},
		{
			name:     "consecutive special chars",
			input:    "Acme@@Corp",
			expected: "acme-corp",
		},
		{
			name:     "unicode with accents",
			input:    "Café",
			expected: "cafe",
		},
		{
			name:     "unicode with diacritics",
			input:    "Société",
			expected: "societe",
		},
		{
			name:     "mixed unicode and special",
			input:    "Société & Cie",
			expected: "societe-cie",
		},
		{
			name:     "numbers preserved",
			input:    "Company 123",
			expected: "company-123",
		},
		{
			name:     "only special chars",
			input:    "!!!",
			expected: "",
		},
		{
			name:     "only spaces",
			input:    "   ",
			expected: "",
		},
		{
			name:     "hyphen preservation",
			input:    "my-company",
			expected: "my-company",
		},
		{
			name:     "consecutive hyphens collapse",
			input:    "my--company",
			expected: "my-company",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeTenantDomain(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateTenantDomain(t *testing.T) {
	t.Run("simple ascii org name", func(t *testing.T) {
		result := GenerateTenantDomain("Acme Corp")
		assert.True(t, strings.HasPrefix(result, "acme-corp-"),
			"should start with slugified org name")
		assert.LessOrEqual(t, len(result), maxTenantNameLength,
			"should not exceed max length")
		assert.NotContains(t, result, "--",
			"should not have consecutive hyphens")
	})

	t.Run("empty org name", func(t *testing.T) {
		result := GenerateTenantDomain("")
		assert.True(t, isValidRandomName(result),
			"should be a valid random name")
	})

	t.Run("unicode org name", func(t *testing.T) {
		result := GenerateTenantDomain("Café International")
		assert.True(t, strings.HasPrefix(result, "cafe-international-"),
			"should start with normalized org name")
		assert.LessOrEqual(t, len(result), maxTenantNameLength)
	})

	t.Run("very long org name", func(t *testing.T) {
		longName := strings.Repeat("a", 100)
		result := GenerateTenantDomain(longName)
		assert.LessOrEqual(t, len(result), maxTenantNameLength,
			"should truncate to max length")
	})

	t.Run("org name that becomes empty after normalization", func(t *testing.T) {
		result := GenerateTenantDomain("!!!")
		assert.True(t, isValidRandomName(result),
			"should fall back to random name")
	})

	t.Run("randomness check", func(t *testing.T) {
		// Generate multiple names and verify they differ
		results := make(map[string]bool)
		for range 10 {
			result := GenerateTenantDomain("Test")
			results[result] = true
		}
		assert.Greater(t, len(results), 1,
			"should produce different random names on repeated calls")
	})

	t.Run("max length enforcement", func(t *testing.T) {
		// Test with various org names to ensure we never exceed max
		testCases := []string{
			"A",
			"Short",
			"This is a very long organization name",
			strings.Repeat("x", 100),
		}
		for _, orgName := range testCases {
			result := GenerateTenantDomain(orgName)
			assert.LessOrEqual(t, len(result), maxTenantNameLength,
				"should enforce max length for %q", orgName)
		}
	})

	t.Run("no leading or trailing hyphens", func(t *testing.T) {
		for range 20 {
			result := GenerateTenantDomain("Test Organization")
			assert.False(t, strings.HasPrefix(result, "-"),
				"should not start with hyphen")
			assert.False(t, strings.HasSuffix(result, "-"),
				"should not end with hyphen")
		}
	})
}

func TestNormalizeTenantDomainUnicode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "NFKD normalization - combined form",
			input:    "é",
			expected: "e",
		},
		{
			name:     "multiple diacritics",
			input:    "naïve",
			expected: "naive",
		},
		{
			name:     "mixed case with diacritics",
			input:    "CAFÉ",
			expected: "cafe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeTenantDomain(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildGeneratedTenantName(t *testing.T) {
	tests := []struct {
		name      string
		orgName   string
		randomStr string
		expected  string
	}{
		{
			name:      "simple case",
			orgName:   "acme",
			randomStr: "alpha-beta-abc",
			expected:  "acme-alpha-beta-abc",
		},
		{
			name:      "empty org name",
			orgName:   "",
			randomStr: "alpha-beta-abc",
			expected:  "alpha-beta-abc",
		},
		{
			name:      "org name becomes empty after normalization",
			orgName:   "!!!",
			randomStr: "alpha-beta-abc",
			expected:  "alpha-beta-abc",
		},
		{
			name:      "truncation needed for very long org",
			orgName:   strings.Repeat("x", 100),
			randomStr: "alpha-beta-abc",
			expected:  strings.Repeat("x", 48) + "-alpha-beta-abc",
		},
		{
			name:      "truncation with edge hyphen trimming",
			orgName:   "test-company-",
			randomStr: strings.Repeat("y", 50),
			expected:  "test-company-" + strings.Repeat("y", 50),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildGeneratedTenantName(tt.orgName, tt.randomStr)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), maxTenantNameLength,
				"should not exceed max length")
		})
	}
}

func TestRandomSuffix(t *testing.T) {
	t.Run("correct length", func(t *testing.T) {
		for length := 1; length <= 10; length++ {
			suffix := randomSuffix(length)
			assert.Equal(t, length, len(suffix))
		}
	})

	t.Run("contains only alphanumeric", func(t *testing.T) {
		for range 20 {
			suffix := randomSuffix(3)
			for _, ch := range suffix {
				assert.True(t, isSlugChar(rune(ch)),
					"should contain only alphanumeric characters")
			}
		}
	})

	t.Run("produces different values", func(t *testing.T) {
		results := make(map[string]bool)
		for range 10 {
			results[randomSuffix(3)] = true
		}
		assert.Greater(t, len(results), 1,
			"should produce different random suffixes")
	})
}

func TestRandomItem(t *testing.T) {
	t.Run("returns item from adjectives", func(t *testing.T) {
		for range 20 {
			item := randomItem(adjectives)
			assert.NotEmpty(t, item)
			assert.True(t, slices.Contains(adjectives, item), "should return an item from the list")
		}
	})

	t.Run("returns item from nouns", func(t *testing.T) {
		for range 20 {
			item := randomItem(nouns)
			assert.NotEmpty(t, item)
			assert.True(t, slices.Contains(nouns, item), "should return an item from the list")
		}
	})
}

func TestGenerateTenantDomainFormat(t *testing.T) {
	for range 20 {
		result := GenerateTenantDomain("TestOrg")
		parts := strings.Split(result, "-")
		require.GreaterOrEqual(t, len(parts), 3,
			"should have at least adjective-noun-suffix")

		suffix := parts[len(parts)-1]
		assert.Equal(t, 3, len(suffix),
			"suffix should be 3 chars")
		for _, ch := range suffix {
			assert.True(t, isSlugChar(ch), "suffix should contain only alphanumeric")
		}
	}
}

func isValidRandomName(s string) bool {
	if s == "" {
		return false
	}
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return false
	}
	return !slices.Contains(parts, "")
}
