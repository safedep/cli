// Package theme defines the SafeDep CLI's dry/tui theme. Theme selection
// is configuration, not a TUI primitive wrapper, so it lives in its own
// package rather than under internal/tui.
package theme

import (
	"github.com/charmbracelet/lipgloss"
	drytheme "github.com/safedep/dry/tui/theme"
)

// CLI returns the safedep-cli theme: only warnings and errors carry color.
// Info and success render with the terminal's default foreground so routine
// messages don't compete visually with actionable ones.
func CLI() drytheme.Theme {
	noColor := lipgloss.AdaptiveColor{Light: "", Dark: ""}

	return drytheme.From(
		drytheme.SafeDep(),
		drytheme.WithName("safedep-cli"),
		drytheme.WithColor(drytheme.RoleInfo, noColor),
		drytheme.WithColor(drytheme.RoleSuccess, noColor),
	)
}
