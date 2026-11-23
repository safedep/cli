package workload

import (
	"errors"
	"fmt"
	"strings"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/rpcerror"
	"github.com/safedep/cli/internal/tui"
)

func newWorkloadError(code, humanError, help string, cause error) error {
	return usefulerror.NewUsefulError().
		WithCode(code).
		WithHumanError(humanError).
		WithHelp(help).
		Wrap(cause)
}

func invalidInputError(humanError, help string) error {
	return newWorkloadError(usefulerror.ErrBadRequest, humanError, help, errors.New(humanError))
}

// mapError gives the recovery for the errors that every workload admin RPC
// can return. command is the command path after "safedep workload", as in
// "rule create".
func mapError(command string, err error) error {
	cause := fmt.Errorf("workload %s: %w", command, err)
	st, ok := status.FromError(err)
	if !ok {
		return cause
	}
	switch st.Code() {
	case codes.PermissionDenied:
		return newWorkloadError(usefulerror.ErrAuthorizationFailed,
			"Only an Admin can manage workload identity",
			"Ask an Admin of the tenant to run the command",
			cause)
	case codes.InvalidArgument:
		// The generic conversion shows only "Invalid request", and the user
		// cannot find the value to fix.
		return newWorkloadError(usefulerror.ErrBadRequest,
			rpcerror.Describe(err, rpcerror.AsIs),
			"See the flags: safedep workload "+command+" --help",
			cause)
	}
	return cause
}

// mapRuleError gives the recovery for a rule that the tenant does not have.
func mapRuleError(command, rule string, err error) error {
	if status.Code(err) == codes.NotFound {
		return newWorkloadError(usefulerror.ErrNotFound,
			fmt.Sprintf("Workload trust rule %s does not exist", tui.EscapeLine(rule)),
			"List the rules: safedep workload rule list",
			fmt.Errorf("workload %s: %w", command, err))
	}
	return mapError(command, err)
}

// mapRuleCreateError gives the recovery for each refusal of a rule create.
// provider is the provider of the rule as the user knows it, as in
// github-actions:acme.
func mapRuleCreateError(name, provider string, err error) error {
	cause := fmt.Errorf("workload rule create: %w", err)
	provider = tui.EscapeLine(provider)
	switch reason, _ := usefulerror.ReasonOf(err); reason {
	case errorv1.ErrorReason_ERROR_REASON_GITHUB_REPOSITORY_NOT_ACCESSIBLE:
		return newWorkloadError(usefulerror.ErrNotFound,
			repositoriesNotAccessible(err),
			fmt.Sprintf("Name only repositories of %s that the SafeDep GitHub App installation can see. "+
				"An owner of the GitHub organization can add a repository to the installation in the GitHub "+
				"settings of the organization", provider),
			cause)
	case errorv1.ErrorReason_ERROR_REASON_GITHUB_INSTALLATION_SUSPENDED:
		return newWorkloadError(usefulerror.ErrConflict,
			fmt.Sprintf("GitHub suspended the SafeDep GitHub App installation of %s", provider),
			"An owner of the GitHub organization can end the suspension in the GitHub settings of the "+
				"organization. Then run the command again",
			cause)
	}
	switch status.Code(err) {
	case codes.NotFound:
		return newWorkloadError(usefulerror.ErrNotFound,
			fmt.Sprintf("Workload identity provider %s is not a provider of the tenant", provider),
			"List the providers: safedep workload provider list",
			cause)
	case codes.FailedPrecondition:
		return newWorkloadError(usefulerror.ErrConflict,
			"The server refused the rule: "+rpcerror.Describe(err, rpcerror.AsIs),
			"Link the GitHub organization again with the SafeDep GitHub App in SafeDep Cloud. "+
				"See the link state: safedep workload provider list",
			cause)
	case codes.AlreadyExists:
		return newWorkloadError(usefulerror.ErrConflict,
			fmt.Sprintf("A workload trust rule with the name %s exists", tui.EscapeLine(name)),
			"See it with: safedep workload rule show "+tui.EscapeLine(name),
			cause)
	case codes.Unavailable:
		// The server sends UNAVAILABLE when GitHub does not answer, but a
		// transport failure or a gateway has the same code, so the text does
		// not name the cause.
		return newWorkloadError(usefulerror.ErrServiceUnavailable,
			rpcerror.Describe(err, rpcerror.AsIs),
			"Run the command again later",
			cause)
	}
	return mapError("rule create", err)
}

// repositoriesNotAccessible lists each repository that the installation
// cannot see. The server names each one in the description of a field
// violation on repositories[i], so the field adds nothing.
func repositoriesNotAccessible(err error) string {
	return rpcerror.Describe(err, func(field, description string) (string, string) {
		if strings.HasPrefix(field, "repositories[") {
			return "", description
		}
		return field, description
	})
}
