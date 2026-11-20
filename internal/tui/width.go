package tui

import (
	"os"

	"golang.org/x/term"
)

// StdoutWidth returns the width of the terminal on stdout, or 0 when stdout
// is not a terminal. A data view wraps to the width of the stream that it
// writes to: dry/tui output.Width reads stderr and falls back to 80
// columns, so a view written to a file would wrap at a width that does not
// apply to it.
func StdoutWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 0
	}
	return w
}
