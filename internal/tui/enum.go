package tui

import (
	"slices"
	"strings"
)

// EnumToken converts a proto enum's String() form into a lowercase, hyphenated
// display token, shared by commands that render proto enums. It strips prefix,
// maps the unspecified/empty value to "unknown", lowercases, and turns
// underscores into hyphens. For example:
//
//	EnumToken("ANALYSIS_STATUS_IN_PROGRESS", "ANALYSIS_STATUS_") -> "in-progress"
//	EnumToken("BILLING_TIER_PROFESSIONAL", "BILLING_TIER_")       -> "professional"
//	EnumToken("ECOSYSTEM_UNSPECIFIED", "ECOSYSTEM_")              -> "unknown"
func EnumToken(enumString, prefix string) string {
	v := strings.TrimPrefix(enumString, prefix)
	if v == "" || v == "UNSPECIFIED" {
		return "unknown"
	}
	return strings.ReplaceAll(strings.ToLower(v), "_", "-")
}

// ParseEnumToken resolves a display token produced by EnumToken back to its
// proto enum number, using the generated `<Enum>_name` map. The unspecified
// value is never resolvable: it is the absence of a selection, so callers
// cannot ask for it by name.
//
//	ParseEnumToken(v1.ScanStatus_name, "SCAN_STATUS_", "queued") -> 3, true
func ParseEnumToken(names map[int32]string, prefix, token string) (int32, bool) {
	for number, name := range names {
		if number == 0 {
			continue
		}
		if EnumToken(name, prefix) == token {
			return number, true
		}
	}
	return 0, false
}

// EnumTokens lists every resolvable display token for a generated
// `<Enum>_name` map, ordered by enum number so help text and error messages
// stay stable across builds. The unspecified value is excluded to match
// ParseEnumToken.
func EnumTokens(names map[int32]string, prefix string) []string {
	numbers := make([]int32, 0, len(names))
	for number := range names {
		if number == 0 {
			continue
		}
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)

	tokens := make([]string, 0, len(numbers))
	for _, number := range numbers {
		tokens = append(tokens, EnumToken(names[number], prefix))
	}
	return tokens
}
