package auth

import (
	"crypto/rand"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var adjectives = []string{
	"amber", "brisk", "calm", "clever", "crisp", "eager", "fable", "gentle", "glad",
	"golden", "grand", "keen", "lively", "mellow", "nimble", "quiet", "rapid", "spruce",
	"steady", "sunny",
}

var nouns = []string{
	"anchor", "beacon", "branch", "bridge", "cloud", "harbor", "grove", "meadow", "orbit",
	"pioneer", "quartz", "ridge", "rocket", "summit", "trail", "vector", "vista", "wave",
	"willow", "yard",
}

const maxTenantNameLength = 63

// NormalizeTenantDomain converts an arbitrary string into a valid tenant
// domain slug. It applies NFKD normalization, strips combining marks, converts
// to lowercase, replaces non-alphanumeric chars with hyphens, collapses runs,
// and trims leading/trailing hyphens.
func NormalizeTenantDomain(s string) string {
	return slugifyOrganizationName(s)
}

// GenerateTenantDomain generates a unique domain slug for an org name by
// combining a slugified version of the org name with a random adjective-noun
// suffix and 3 random alphanumeric characters.
func GenerateTenantDomain(orgName string) string {
	randomName := generateRandomTenantName()
	return buildGeneratedTenantName(orgName, randomName)
}

func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	s = norm.NFKD.String(s)
	var result strings.Builder
	for _, r := range s {
		if !isCombiningMark(r) {
			result.WriteRune(r)
		}
	}
	return strings.ToLower(result.String())
}

func isCombiningMark(r rune) bool {
	return r >= 0x0300 && r <= 0x036F
}

func slugifyOrganizationName(s string) string {
	s = normalizeText(s)
	s = replaceNonAlphanumeric(s)
	s = collapseRuns(s)
	s = trimEdgeHyphens(s)
	return s
}

func replaceNonAlphanumeric(s string) string {
	var result strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			result.WriteRune(r)
		case r >= '0' && r <= '9':
			result.WriteRune(r)
		case r == '-':
			result.WriteRune(r)
		default:
			result.WriteRune('-')
		}
	}
	return result.String()
}

func isAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
}

func collapseRuns(s string) string {
	var result strings.Builder
	var lastWasHyphen bool
	for _, r := range s {
		if r == '-' {
			if !lastWasHyphen {
				result.WriteRune(r)
				lastWasHyphen = true
			}
		} else {
			result.WriteRune(r)
			lastWasHyphen = false
		}
	}
	return result.String()
}

func trimEdgeHyphens(s string) string {
	return strings.Trim(s, "-")
}

func generateRandomTenantName() string {
	adj := randomItem(adjectives)
	noun := randomItem(nouns)
	suffix := randomSuffix(3)
	return adj + "-" + noun + "-" + suffix
}

func randomItem(items []string) string {
	if len(items) == 0 {
		return ""
	}
	b := make([]byte, 1)
	if _, err := rand.Read(b); err != nil {
		return items[0]
	}
	idx := int(b[0]) % len(items)
	return items[idx]
}

func randomSuffix(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("a", length)
	}
	for i := 0; i < length; i++ {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func buildGeneratedTenantName(orgName, randomName string) string {
	orgSlug := slugifyOrganizationName(orgName)
	if orgSlug == "" {
		return randomName
	}

	maxOrgSlugLength := maxTenantNameLength - len(randomName) - 1
	if maxOrgSlugLength <= 0 {
		return randomName
	}

	if maxOrgSlugLength > len(orgSlug) {
		maxOrgSlugLength = len(orgSlug)
	}

	truncatedSlug := orgSlug[:maxOrgSlugLength]
	truncatedSlug = trimEdgeHyphens(truncatedSlug)

	if truncatedSlug == "" {
		return randomName
	}

	return truncatedSlug + "-" + randomName
}
