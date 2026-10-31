package tui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text from SafeDep Cloud, such as a label that an endpoint sent or the
// reason of a package rule, can hold escape sequences that move the cursor,
// change the window title or hide text. Every table and plain renderer
// passes such text through these helpers. JSON output stays raw, because the
// JSON encoder escapes control characters itself. dry/tui has no escape
// helper yet, so the CLI keeps one here, with the API of vet's
// internal/tui/escape.

// EscapeLine returns s as one line. It shows each control character, line
// breaks and tabs included, and each bidirectional format character as an
// escape, for example \n, \x1b or \u202e. Invalid UTF-8 becomes U+FFFD.
func EscapeLine(s string) string {
	return escape(s, false)
}

// EscapeText returns s with its line breaks kept. It escapes every other
// control character and each bidirectional format character.
func EscapeText(s string) string {
	return escape(s, true)
}

// EscapeCells escapes each cell of each table row as one line.
func EscapeCells(rows [][]string) [][]string {
	out := make([][]string, len(rows))
	for i, row := range rows {
		out[i] = make([]string, len(row))
		for j, cell := range row {
			out[i][j] = EscapeLine(cell)
		}
	}
	return out
}

// PlainRow escapes each field as one line and joins the fields with tabs, so
// a tab or a line break in a value never adds a column or a row.
func PlainRow(fields ...string) string {
	escaped := make([]string, len(fields))
	for i, f := range fields {
		escaped[i] = EscapeLine(f)
	}
	return strings.Join(escaped, "\t")
}

func escape(s string, keepNewlines bool) string {
	if isSafe(s, keepNewlines) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\n' && keepNewlines:
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == unicode.ReplacementChar:
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case unsafeRune(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isSafe(s string, keepNewlines bool) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '\n' && keepNewlines {
			continue
		}
		if r < 0x20 || r == 0x7f || unsafeRune(r) {
			return false
		}
	}
	return true
}

// unsafeRune reports a C1 control character or a bidirectional format
// character. A bidirectional override can show text in another order than
// the bytes, as in the "Trojan Source" attack.
func unsafeRune(r rune) bool {
	switch {
	case r >= 0x80 && r <= 0x9f:
		return true
	case r == 0x061c, r == 0x200e, r == 0x200f:
		return true
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}
