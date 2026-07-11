package endpoint

import (
	"github.com/oklog/ulid/v2"
	"github.com/safedep/dry/tui/humanize"
)

// isULID reports whether s is a syntactically valid ULID (26-character
// Crockford base32). ParseStrict (not Parse) rejects the lenient
// substitutions that Parse silently performs.
func isULID(s string) bool {
	_, err := ulid.ParseStrict(s)
	return err == nil
}

// humanWindowLabel is the table-mode variant of TimeWindow.Label with a
// compact duration ("last 7d" instead of "last 168h0m0s"). Plain and JSON
// renderings keep Label() unchanged.
func humanWindowLabel(w TimeWindow) string {
	if w.Start.IsZero() || w.End.IsZero() {
		return "server default"
	}
	return "last " + humanize.Duration(w.End.Sub(w.Start))
}
