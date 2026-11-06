package project

import (
	"fmt"
	"strings"

	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc/status"
)

const (
	refPrefix       = "refs/"
	branchRefPrefix = "refs/heads/"
)

// qualifiedRef turns the --ref value into the full ref the API requires. A plain
// name is a branch, as in "git switch <name>". A tag needs refs/tags/ because a
// name can be a branch and a tag at once, and the server rejects that pair
// rather than guess. A value under refs/ goes through unchanged so the server
// owns the ref name rules.
func qualifiedRef(ref string) string {
	if strings.HasPrefix(ref, refPrefix) {
		return ref
	}
	return branchRefPrefix + ref
}

func validateRef(ref string, set bool) error {
	if set && ref == "" {
		return newProjectError(
			usefulerror.ErrBadRequest,
			"Invalid ref",
			"Pass a branch name, refs/heads/<branch> or refs/tags/<tag>, or omit --ref to scan the default branch.",
			fmt.Errorf("--ref must not be empty"),
		)
	}
	return nil
}

// refRejectedError explains a server rejection of the requested ref. The
// generic InvalidArgument rendering hides the server message behind
// --verbose, and that message names the project and the ref that failed.
func refRejectedError(err error, ref string) error {
	return newProjectError(
		usefulerror.ErrBadRequest,
		"Ref cannot be scanned",
		fmt.Sprintf(
			"%s. Check that %s exists in every selected repository. A plain name is a branch: pass refs/tags/<tag> for a tag.",
			status.Convert(err).Message(),
			ref,
		),
		fmt.Errorf("project scan create: %w", err),
	)
}
