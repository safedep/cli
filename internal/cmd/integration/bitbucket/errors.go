package bitbucket

import (
	"github.com/safedep/dry/usefulerror"
)

func invalidSelectionError(cause error, help string) error {
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrBadRequest).
		WithHumanError("Invalid allowlist selection").
		WithHelp(help).
		Wrap(cause)
}
