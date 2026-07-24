package tui

import (
	"os"

	"github.com/safedep/dry/tui/output"
	"golang.org/x/term"
)

// IsInteractive reports whether the CLI can prompt the user and open a browser.
// It keys off the actual terminal state (stdin is a TTY, which prompt libraries
// need to read input) rather than the presentation mode, which is user-
// overridable via --output/SAFEDEP_OUTPUT and does not reflect whether a human
// is present. Agent mode is excluded outright so tool-driven runs never block
// on stdin.
func IsInteractive() bool {
	if output.CurrentMode() == output.Agent {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}
