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
	tagRefPrefix    = "refs/tags/"

	refFlag    = "ref"
	branchFlag = "branch"
	tagFlag    = "tag"
)

// refFlags holds the three ways to name the ref to scan. Set records which
// flags the user passed, so an empty value is told apart from an absent flag.
type refFlags struct {
	Ref    string
	Branch string
	Tag    string
	Set    []string
}

// qualified is the full ref the API requires, or "" to scan the default
// branch. --branch and --tag name the namespace, so the CLI never guesses.
// A plain --ref name is a branch, as in "git switch <name>", and a full --ref
// goes through unchanged so the server owns the ref name rules.
func (f refFlags) qualified() (string, error) {
	switch len(f.Set) {
	case 0:
		return "", nil
	case 1:
	default:
		return "", invalidRefError(
			fmt.Errorf("--%s cannot be combined", strings.Join(f.Set, ", --")),
			"Pass only one of --branch, --tag or --ref.",
		)
	}

	switch f.Set[0] {
	case branchFlag:
		return shortRef(branchFlag, f.Branch, branchRefPrefix)
	case tagFlag:
		return shortRef(tagFlag, f.Tag, tagRefPrefix)
	}
	if f.Ref == "" {
		return "", invalidRefError(
			fmt.Errorf("--ref must not be empty"),
			"Pass a branch name, refs/heads/<branch> or refs/tags/<tag>, or omit --ref to scan the default branch.",
		)
	}
	if strings.HasPrefix(f.Ref, refPrefix) {
		return f.Ref, nil
	}
	return branchRefPrefix + f.Ref, nil
}

func shortRef(flag, name, prefix string) (string, error) {
	if name == "" {
		return "", invalidRefError(
			fmt.Errorf("--%s must not be empty", flag),
			fmt.Sprintf("Pass a %s name, or omit --%s to scan the default branch.", flag, flag),
		)
	}
	if strings.HasPrefix(name, refPrefix) {
		return "", invalidRefError(
			fmt.Errorf("--%s takes a short name, not %q", flag, name),
			fmt.Sprintf("Pass the name without refs/, or pass the full ref with --ref %s.", name),
		)
	}
	return prefix + name, nil
}

func invalidRefError(cause error, help string) error {
	return newProjectError(usefulerror.ErrBadRequest, "Invalid ref", help, cause)
}

// refRejectedError explains a server rejection of the requested ref. The
// generic InvalidArgument rendering hides the server message behind
// --verbose, and that message names the project and the ref that failed.
func refRejectedError(err error, ref string) error {
	help := fmt.Sprintf("%s. Check that %s exists in every selected repository.", status.Convert(err).Message(), ref)
	if name, ok := strings.CutPrefix(ref, branchRefPrefix); ok {
		help += fmt.Sprintf(" If %s is a tag, pass --tag %s.", name, name)
	}
	return newProjectError(
		usefulerror.ErrBadRequest,
		"Ref cannot be scanned",
		help,
		fmt.Errorf("project scan create: %w", err),
	)
}
