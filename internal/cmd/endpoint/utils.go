package endpoint

import "github.com/oklog/ulid/v2"

// isULID reports whether s is a syntactically valid ULID (26-character
// Crockford base32). ParseStrict (not Parse) rejects the lenient
// substitutions that Parse silently performs.
func isULID(s string) bool {
	_, err := ulid.ParseStrict(s)
	return err == nil
}
