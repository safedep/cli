package project

import (
	"fmt"
	"strings"

	"github.com/safedep/dry/usefulerror"
)

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

func invalidRepositorySelectionError(cause error, help string) error {
	return newProjectError(
		usefulerror.ErrBadRequest,
		"Invalid repository selection",
		help,
		cause,
	)
}

func unknownFilterValueError(flag, value string, allowed []string) error {
	choices := strings.Join(allowed, ", ")
	cause := fmt.Errorf("unknown --%s value %q: allowed values are %s", flag, value, choices)
	return newProjectError(
		usefulerror.ErrBadRequest,
		"Invalid filter value",
		fmt.Sprintf("Retry --%s with one of: %s.", flag, choices),
		cause,
	)
}
