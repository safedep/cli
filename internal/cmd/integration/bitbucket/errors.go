package bitbucket

import (
	"github.com/safedep/dry/usefulerror"
)

func newBitbucketError(code, humanError, help string, cause error) error {
	return usefulerror.NewUsefulError().
		WithCode(code).
		WithHumanError(humanError).
		WithHelp(help).
		Wrap(cause)
}

func invalidSelectionError(cause error, help string) error {
	return newBitbucketError(
		usefulerror.ErrBadRequest,
		"Invalid allowlist selection",
		help,
		cause,
	)
}
