package tui

import (
	"github.com/charmbracelet/lipgloss"
	drytheme "github.com/safedep/dry/tui/theme"
)

// CLITheme returns the safedep-cli theme. main() pushes this into
// dry/tui as the global default at startup, after which dry/tui Console
// helpers (Info / Success / Warning / Error / Print) pick it up.
//
// Only warnings and errors carry color. Info and success render with
// the terminal's default foreground so routine messages do not compete
// visually with actionable ones.
func CLITheme() drytheme.Theme {
	noColor := lipgloss.AdaptiveColor{Light: "", Dark: ""}
	return drytheme.From(
		drytheme.SafeDep(),
		drytheme.WithName("safedep-cli"),
		drytheme.WithColor(drytheme.RoleInfo, noColor),
		drytheme.WithColor(drytheme.RoleSuccess, noColor),
	)
}
