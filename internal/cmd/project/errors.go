package project

import "github.com/safedep/dry/usefulerror"

func newProjectError(code, humanError, help string, cause error) error {
	return usefulerror.NewUsefulError().
		WithCode(code).
		WithHumanError(humanError).
		WithHelp(help).
		Wrap(cause)
}

func invalidProjectSelectionError(cause error, help string) error {
	return newProjectError(
		usefulerror.ErrBadRequest,
		"Invalid project selection",
		help,
		cause,
	)
}
