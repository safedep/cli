package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/dry/tui/theme"
)

// CLITheme returns the safedep-cli theme: only warnings and errors carry color.
// Info and success render with the terminal's default foreground so routine
// messages don't compete visually with actionable ones.
func CLITheme() theme.Theme {
	noColor := lipgloss.AdaptiveColor{Light: "", Dark: ""}

	return theme.From(
		theme.SafeDep(),
		theme.WithName("safedep-cli"),
		theme.WithColor(theme.RoleInfo, noColor),
		theme.WithColor(theme.RoleSuccess, noColor),
	)
}
