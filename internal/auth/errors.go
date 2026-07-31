package auth

import "github.com/safedep/dry/usefulerror"

// LoginRequiredError gives all CLI authentication failures one recovery path.
func LoginRequiredError(cause error) error {
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrAuthenticationFailed).
		WithHumanError("Authentication required").
		WithHelp("Run `safedep auth login` and retry.").
		Wrap(cause)
}
