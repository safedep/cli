package tui

import "strings"

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
